package cs

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cs "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	csRes "github.com/flipped-aurora/gin-vue-admin/server/model/cs/response"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var phoneRe = regexp.MustCompile(`^1[3-9]\d{9}$`)

var (
	ErrTenantNotFound   = errors.New("租户不存在")
	ErrTenantClosed     = errors.New("租户已停用")
	ErrAccountNotFound  = errors.New("账号不存在")
	ErrAccountDisabled  = errors.New("账号已禁用")
	ErrPasswordFail     = errors.New("账号或密码错误")
	ErrRoleMismatch     = errors.New("账号角色与当前端不匹配")
	ErrSmsNotConfigured = errors.New("账号未绑定手机号，无法接收短信验证码")
)

type TenantService struct{}

// GetActiveTenantByCode 商户端登录按租户编码定位并校验有效
func (s *TenantService) GetActiveTenantByCode(code string) (*cs.CsTenant, error) {
	var t cs.CsTenant
	err := global.GVA_DB.Where("code = ? AND status = ?", code, cs.TenantStatusNormal).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TenantPublic H5 渠道接入前公开查询商户信息; 停用租户也返回状态供前端提示
func (s *TenantService) TenantPublic(code string) (*csRes.TenantPublicRes, error) {
	var t cs.CsTenant
	err := global.GVA_DB.Where("code = ?", code).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, err
	}
	return &csRes.TenantPublicRes{Code: t.Code, Name: t.Name, Status: t.Status, WelcomeTemplate: t.WelcomeTemplate}, nil
}

type AgentService struct{}

// GetByUsername 客服/商户管理员 登录名全局唯一
func (s *AgentService) GetByUsername(username string) (*cs.CsAgent, error) {
	var a cs.CsAgent
	err := global.GVA_DB.Where("username = ?", username).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetActiveAgent 校验账号可用 + 归属租户有效 + 角色与端匹配
func (s *AgentService) GetActiveAgent(username, scope string) (*cs.CsAgent, *cs.CsTenant, error) {
	a, err := s.GetByUsername(username)
	if err != nil {
		return nil, nil, err
	}
	if a.Enable != cs.AccountStatusNormal {
		return nil, nil, ErrAccountDisabled
	}
	switch scope {
	case cs.ScopeMerchant:
		if a.Role != cs.AgentRoleMerchantAdmin {
			return nil, nil, ErrRoleMismatch
		}
	case cs.ScopeAgent:
		if a.Role != cs.AgentRoleAgent && a.Role != cs.AgentRoleSupervisor {
			return nil, nil, ErrRoleMismatch
		}
	default:
		return nil, nil, fmt.Errorf("未知端: %s", scope)
	}
	var ts TenantService
	tenant, err := ts.GetActiveByID(a.TenantID)
	if err != nil {
		return nil, nil, err
	}
	return a, tenant, nil
}

func (s *TenantService) GetActiveByID(id uint) (*cs.CsTenant, error) {
	var t cs.CsTenant
	err := global.GVA_DB.First(&t, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, err
	}
	if t.Status != cs.TenantStatusNormal {
		return nil, ErrTenantClosed
	}
	return &t, nil
}

// CheckPassword bcrypt 校验 失败返回统一错误避免账号枚举
func CheckPassword(hashed, plain string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)); err != nil {
		return ErrPasswordFail
	}
	return nil
}

// HashPassword 生成 bcrypt 散列
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

// ValidPhone 手机号格式校验
func ValidPhone(phone string) bool { return phoneRe.MatchString(phone) }

type MemberService struct{}

func (s *MemberService) GetActiveByUsername(username string) (*cs.CsMember, error) {
	var m cs.CsMember
	err := global.GVA_DB.Where("username = ?", username).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, err
	}
	if m.Status != cs.AccountStatusNormal {
		return nil, ErrAccountDisabled
	}
	return &m, nil
}
