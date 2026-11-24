package cs

import (
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
)

// 文件扫描状态
const (
	FileScanPending  = "pending"  // 待扫描
	FileScanScanning = "scanning" // 扫描中
	FileScanClean    = "clean"    // 通过
	FileScanInfected = "infected" // 命中违规(禁止下载)
)

// CsFileAttachment 会话附件。存储由服务端代理(本地/OSS), 下载走短时效签名 URL。
type CsFileAttachment struct {
	global.GVA_MODEL
	TenantID       uint       `json:"tenantId" gorm:"index;not null;comment:租户ID"`
	ConversationID uint       `json:"conversationId" gorm:"index;not null;comment:会话ID"`
	UploaderType   string     `json:"uploaderType" gorm:"size:16;not null;comment:member/agent"`
	UploaderID     uint       `json:"uploaderId" gorm:"not null;comment:上传者账号ID"`
	FileName       string     `json:"fileName" gorm:"size:256;not null;comment:原始文件名"`
	StoredName     string     `json:"storedName" gorm:"size:256;comment:落盘文件名"`
	FilePath       string     `json:"filePath" gorm:"size:512;comment:存储相对路径"`
	Size           int64      `json:"size" gorm:"comment:字节数"`
	MimeType       string     `json:"mimeType" gorm:"size:128;comment:MIME类型"`
	SHA256         string     `json:"sha256" gorm:"size:64;comment:内容摘要"`
	ScanStatus     string     `json:"scanStatus" gorm:"size:16;default:pending;index;comment:pending/scanning/clean/infected"`
	ScanAt         *time.Time `json:"scanAt" gorm:"comment:扫描完成时间"`
	ScanReason     string     `json:"scanReason" gorm:"size:256;comment:违规原因"`
}

func (CsFileAttachment) TableName() string { return "cs_file_attachments" }
