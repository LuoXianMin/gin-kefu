package cs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cs "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	csRes "github.com/flipped-aurora/gin-vue-admin/server/model/cs/response"
	"github.com/flipped-aurora/gin-vue-admin/server/utils/upload"
)

// FileService 会话附件: 服务端代理上传 → 内容扫描状态机 → 短时效签名下载。
// 方案 §H: 禁用 MinIO; 开发环境 oss-type=local, 生产切 aliyun-oss 等不改本服务逻辑(仅换 upload.NewOss)。
type FileService struct{}

var (
	ErrFileTooLarge    = errors.New("文件超过大小限制(10MB)")
	ErrFileTypeDenied  = errors.New("不支持的文件类型")
	ErrFileNotFound    = errors.New("文件不存在")
	ErrFileNotReady    = errors.New("文件扫描未完成, 暂不可下载")
	ErrFileInfected    = errors.New("文件未通过安全扫描")
	ErrBadSignature    = errors.New("下载链接无效或已过期")
	ErrUploadDuplicate = errors.New("同会话已上传过相同内容的文件")
)

const (
	maxFileSize   = 10 << 20 // 10MB
	downloadTTL   = 5 * time.Minute
	scanReadLimit = 1 << 20 // 扫描采样前1MB
)

// allowedFileExts 白名单: 图片/常见文档/压缩包/文本
var allowedFileExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".zip": true, ".txt": true, ".log": true,
}

// Upload 授权(会话成员+未结束) → 类型/大小校验 → 落盘 → 建元数据(pending) → 同步扫描(开发版)。
func (s *FileService) Upload(tenantID uint, uploaderType string, uploaderID, convID uint, file *multipart.FileHeader) (*csRes.FileRes, error) {
	conv, allowed := (&ConversationService{}).CanAccess(tenantID, uploaderType, uploaderID, convID)
	if !allowed {
		return nil, ErrConvNotFound
	}
	if conv.Status == cs.ConvStatusClosed {
		return nil, ErrConvClosed
	}
	if file.Size > maxFileSize {
		return nil, ErrFileTooLarge
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !allowedFileExts[ext] {
		return nil, ErrFileTypeDenied
	}

	// 读内容算 sha256(同时供秒传判重/验签)。大文件流式处理, 此处上限10MB直接全读。
	f, err := file.Open()
	if err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	_ = f.Close()
	if err != nil {
		return nil, err
	}
	if len(buf) > maxFileSize {
		return nil, ErrFileTooLarge
	}
	sum := sha256.Sum256(buf)
	sha := hex.EncodeToString(sum[:])
	var dup int64
	global.GVA_DB.Model(&cs.CsFileAttachment{}).
		Where("conversation_id = ? AND sha256 = ?", convID, sha).Count(&dup)
	if dup > 0 {
		return nil, ErrUploadDuplicate
	}

	path, stored, err := upload.NewOss().UploadFile(file)
	if err != nil {
		return nil, err
	}
	rec := cs.CsFileAttachment{
		TenantID: tenantID, ConversationID: convID,
		UploaderType: uploaderType, UploaderID: uploaderID,
		FileName: filepath.Base(file.Filename), StoredName: stored, FilePath: path,
		Size: file.Size, MimeType: file.Header.Get("Content-Type"), SHA256: sha,
		ScanStatus: cs.FileScanPending,
	}
	if err := global.GVA_DB.Create(&rec).Error; err != nil {
		return nil, err
	}

	// 开发扫描器: EICAR 特征 + 空文件拦截; 生产替换为异步 AV 回调(UpdateScanResult)
	clean, reason := scanDev(buf)
	status := cs.FileScanClean
	if !clean {
		status = cs.FileScanInfected
	}
	now := time.Now().UTC()
	if err := global.GVA_DB.Model(&rec).Updates(map[string]interface{}{
		"scan_status": status, "scan_at": now, "scan_reason": reason,
	}).Error; err != nil {
		return nil, err
	}
	rec.ScanStatus = status
	rec.ScanAt = &now
	rec.ScanReason = reason
	MetricIncr(context.Background(), MetricFileUploaded, uploaderType)
	if !clean {
		MetricIncr(context.Background(), MetricFileInfected, uploaderType)
		Audit(tenantID, uploaderType, uploaderID, "file.infected", fmt.Sprintf("file:%d", rec.ID), reason, "")
	}
	return toFileRes(&rec), nil
}

// UpdateScanResult 供外部 AV 扫描回调更新状态(生产接入点)
func (s *FileService) UpdateScanResult(fileID uint, clean bool, reason string) error {
	now := time.Now().UTC()
	status := cs.FileScanClean
	if !clean {
		status = cs.FileScanInfected
	}
	return global.GVA_DB.Model(&cs.CsFileAttachment{}).Where("id = ?", fileID).
		Updates(map[string]interface{}{"scan_status": status, "scan_at": now, "scan_reason": reason}).Error
}

// IssueDownloadURL 权限校验(会话成员) + 扫描通过 → 签发5分钟HMAC短链
func (s *FileService) IssueDownloadURL(tenantID uint, scope string, accountID, fileID uint) (*csRes.FileURLRes, error) {
	file, err := s.getFileForScope(tenantID, scope, accountID, fileID)
	if err != nil {
		return nil, err
	}
	// 签发层拦截: 违规文件永不发链, 未扫描完成也不发(避免下载层窗口期绕过)
	if file.ScanStatus == cs.FileScanInfected {
		return nil, ErrFileInfected
	}
	if file.ScanStatus != cs.FileScanClean {
		return nil, ErrFileNotReady
	}
	exp := time.Now().UTC().Add(downloadTTL).Unix()
	sig := signFileURL(fileID, exp)
	return &csRes.FileURLRes{
		FileID: fileID, FileName: file.FileName,
		URL:     fmt.Sprintf("/cs/files/dl?fid=%d&exp=%d&sig=%s", fileID, exp, sig),
		ExpireAt: time.Unix(exp, 0).UTC(),
	}, nil
}

// ResolveDownload 校验签名与时效, 返回可服务的文件(公开端点专用, 权限已在签发时校验)
func (s *FileService) ResolveDownload(fileID uint, exp int64, sig string) (*cs.CsFileAttachment, error) {
	want := signFileURL(fileID, exp)
	if subtle.ConstantTimeCompare([]byte(want), []byte(sig)) != 1 {
		return nil, ErrBadSignature
	}
	if time.Now().UTC().Unix() > exp {
		return nil, ErrBadSignature
	}
	var file cs.CsFileAttachment
	if err := global.GVA_DB.First(&file, fileID).Error; err != nil {
		return nil, ErrFileNotFound
	}
	if file.ScanStatus == cs.FileScanInfected {
		return nil, ErrFileInfected
	}
	if file.ScanStatus != cs.FileScanClean {
		return nil, ErrFileNotReady
	}
	return &file, nil
}

// Info 查询文件状态(会话成员可见)
func (s *FileService) Info(tenantID uint, scope string, accountID, fileID uint) (*csRes.FileRes, error) {
	file, err := s.getFileForScope(tenantID, scope, accountID, fileID)
	if err != nil {
		return nil, err
	}
	return toFileRes(file), nil
}

// getFileForScope 文件定位 + 租户一致 + 会话成员校验(防 IDOR: 不能凭 fileID 读别人会话附件)
func (s *FileService) getFileForScope(tenantID uint, scope string, accountID, fileID uint) (*cs.CsFileAttachment, error) {
	var file cs.CsFileAttachment
	if err := global.GVA_DB.Where("id = ? AND tenant_id = ?", fileID, tenantID).First(&file).Error; err != nil {
		return nil, ErrFileNotFound
	}
	if _, allowed := (&ConversationService{}).CanAccess(tenantID, scope, accountID, file.ConversationID); !allowed {
		return nil, ErrFileNotFound // 不区分"不存在/无权限", 避免探测
	}
	return &file, nil
}

// signFileURL HMAC(JWT签名密钥, "cs-file:{id}:{exp}") — 与令牌同源密钥, 仅服务端可签发
func signFileURL(fileID uint, exp int64) string {
	mac := hmac.New(sha256.New, []byte(global.GVA_CONFIG.JWT.SigningKey))
	mac.Write([]byte(fmt.Sprintf("cs-file:%d:%d", fileID, exp)))
	return hex.EncodeToString(mac.Sum(nil))
}

// scanDev 开发版扫描器: EICAR 标准测试特征命中即判违规
func scanDev(buf []byte) (bool, string) {
	if len(buf) == 0 {
		return false, "空文件"
	}
	if strings.Contains(string(buf), "X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR") {
		return false, "命中病毒测试特征(EICAR)"
	}
	return true, ""
}

func toFileRes(f *cs.CsFileAttachment) *csRes.FileRes {
	return &csRes.FileRes{
		ID: f.ID, ConversationID: f.ConversationID, FileName: f.FileName,
		Size: f.Size, MimeType: f.MimeType, SHA256: f.SHA256,
		ScanStatus: f.ScanStatus, CreatedAt: f.CreatedAt,
	}
}
