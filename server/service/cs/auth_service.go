package cs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cs "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	csReq "github.com/flipped-aurora/gin-vue-admin/server/model/cs/request"
	csRes "github.com/flipped-aurora/gin-vue-admin/server/model/cs/response"
	sysModel "github.com/flipped-aurora/gin-vue-admin/server/model/system"
	"github.com/flipped-aurora/gin-vue-admin/server/utils/cstoken"
	"gorm.io/gorm"
)

// AuthService 客服系统认证用例(平台/商户/客服端 MFA，用户端账密)
type AuthService struct{}

var (
	ErrLoginLocked   = errors.New("账号已锁定，请稍后重试")
	ErrTenantMismatch = errors.New("账号与租户不匹配")
)

// loginFailKey 登录失败计数键(Redis)
func loginFailKey(scope, identity string) string {
	return fmt.Sprintf("cs:login:fail:%s:%s", scope, identity)
}

// checkLock 登录前检查是否处于锁定
func (s *AuthService) checkLock(ctx context.Context, scope, identity string) error {
	redis := global.GVA_REDIS
	if redis == nil {
		return nil
	}
	ttl, err := redis.TTL(ctx, loginFailKey(scope, identity)).Result()
	if err != nil || ttl < 0 {
		return nil
	}
	cnt, _ := redis.Get(ctx, loginFailKey(scope, identity)).Int()
	if cnt >= 5 {
		return ErrLoginLocked
	}
	return nil
}

// recordFail 记录一次登录失败, 达到上限后锁定 15 分钟
func (s *AuthService) recordFail(ctx context.Context, scope, identity string) {
	redis := global.GVA_REDIS
	if redis == nil {
		return
	}
	n, err := redis.Incr(ctx, loginFailKey(scope, identity)).Result()
	if err == nil && n == 1 {
		_ = redis.Expire(ctx, loginFailKey(scope, identity), 15*time.Minute).Err()
	}
}

// clearFail 登录成功清除失败计数
func (s *AuthService) clearFail(ctx context.Context, scope, identity string) {
	redis := global.GVA_REDIS
	if redis == nil {
		return
	}
	_ = redis.Del(ctx, loginFailKey(scope, identity)).Err()
}

var captchaSvc = &CaptchaService{}
var smsSvc = &SmsCodeService{}
var tenantSvc = &TenantService{}
var agentSvc = &AgentService{}
var memberSvc = &MemberService{}

// SendSmsCode 三端 MFA 获取短信验证码
// 安全: 手机号由服务端按 scope+账号反查, 拒绝客户端任意指定; 需先通过图形验证码
func (s *AuthService) SendSmsCode(ctx context.Context, scope string, req csReq.SendSmsCodeReq) (*csRes.SendSmsCodeRes, error) {
	if err := captchaSvc.Verify(req.CaptchaId, req.Captcha); err != nil {
		return nil, err
	}
	phone, identity, err := s.resolvePhone(scope, req.Username, req.TenantCode)
	if err != nil {
		// 为避免账号枚举, 统一返回成功语义不明显暴露账号是否存在, 但开发期保留可诊断错误
		return nil, err
	}
	return smsSvc.Send(ctx, scope, identity, phone)
}

// resolvePhone 按端反查绑定手机号
func (s *AuthService) resolvePhone(scope, username, tenantCode string) (phone, identity string, err error) {
	switch scope {
	case cs.ScopePlatform:
		var u sysModel.SysUser
		if e := global.GVA_DB.Where("username = ?", username).First(&u).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return "", "", ErrAccountNotFound
			}
			return "", "", e
		}
		if u.Phone == "" {
			return "", "", ErrSmsNotConfigured
		}
		return u.Phone, fmt.Sprintf("platform:%d", u.ID), nil
	case cs.ScopeMerchant, cs.ScopeAgent:
		a, tenant, e := agentSvc.GetActiveAgent(username, scope)
		if e != nil {
			return "", "", e
		}
		if tenantCode != "" && tenant.Code != tenantCode {
			return "", "", ErrTenantMismatch
		}
		if a.Phone == "" {
			return "", "", ErrSmsNotConfigured
		}
		return a.Phone, fmt.Sprintf("%s:%d", scope, a.ID), nil
	default:
		return "", "", fmt.Errorf("未知端: %s", scope)
	}
}

// PlatformLogin 平台总后台登录: 账密 + 图形验证码 + 短信验证码
func (s *AuthService) PlatformLogin(ctx context.Context, req csReq.MfaLoginReq) (*csRes.PlatformLoginRes, error) {
	identity := req.Username
	if err := s.checkLock(ctx, cs.ScopePlatform, identity); err != nil {
		return nil, err
	}
	if err := captchaSvc.Verify(req.CaptchaId, req.Captcha); err != nil {
		return nil, err
	}
	var u sysModel.SysUser
	if err := global.GVA_DB.Where("username = ?", req.Username).First(&u).Error; err != nil {
		s.recordFail(ctx, cs.ScopePlatform, identity)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPasswordFail
		}
		return nil, err
	}
	if u.Enable != 1 {
		return nil, ErrAccountDisabled
	}
	if err := CheckPassword(u.Password, req.Password); err != nil {
		s.recordFail(ctx, cs.ScopePlatform, identity)
		return nil, err
	}
	// 短信校验(反查手机号, 与发送时同一 identity)
	smsIdentity := fmt.Sprintf("platform:%d", u.ID)
	if err := smsSvc.Verify(ctx, cs.ScopePlatform, smsIdentity, req.SmsCode); err != nil {
		s.recordFail(ctx, cs.ScopePlatform, identity)
		return nil, err
	}
	s.clearFail(ctx, cs.ScopePlatform, identity)

	token, expires, err := cstoken.CreateToken(cs.ScopePlatform, u.ID, 0, u.Username, "", u.AuthorityId)
	if err != nil {
		return nil, err
	}
	return &csRes.PlatformLoginRes{
		CsTokenRes: csRes.CsTokenRes{Token: token, Expires: expires},
		CsUser: &csRes.PlatformUserInfo{
			ID: u.ID, Username: u.Username, NickName: u.NickName,
			AuthorityId: u.AuthorityId, Phone: maskPhone(u.Phone),
		},
	}, nil
}

// SuperAdminAuthorityId 框架超级管理员角色ID(与 sys_users 默认角色一致)
const SuperAdminAuthorityId = 888

// PlatformExchange 框架已登录态免 MFA 换取 cs 平台令牌(GVA 后台进入客服管理页)
// 仅超级管理员可换; 仍校验账号启用; 与 MFA 登录同域同权限, 不降低安全水位
func (s *AuthService) PlatformExchange(userID, authorityID uint) (*csRes.PlatformLoginRes, error) {
	if authorityID != SuperAdminAuthorityId {
		return nil, errors.New("仅超级管理员可换取客服系统平台令牌")
	}
	var u sysModel.SysUser
	if err := global.GVA_DB.First(&u, userID).Error; err != nil {
		return nil, ErrAccountNotFound
	}
	if u.Enable != 1 {
		return nil, ErrAccountDisabled
	}
	token, expires, err := cstoken.CreateToken(cs.ScopePlatform, u.ID, 0, u.Username, "", u.AuthorityId)
	if err != nil {
		return nil, err
	}
	return &csRes.PlatformLoginRes{
		CsTokenRes: csRes.CsTokenRes{Token: token, Expires: expires},
		CsUser: &csRes.PlatformUserInfo{
			ID: u.ID, Username: u.Username, NickName: u.NickName,
			AuthorityId: u.AuthorityId, Phone: maskPhone(u.Phone),
		},
	}, nil
}

// MerchantLogin 商户端登录: 账密 + 图形验证码 + 短信验证码
func (s *AuthService) MerchantLogin(ctx context.Context, req csReq.MfaLoginReq) (*csRes.MerchantLoginRes, error) {
	identity := req.Username
	if err := s.checkLock(ctx, cs.ScopeMerchant, identity); err != nil {
		return nil, err
	}
	if err := captchaSvc.Verify(req.CaptchaId, req.Captcha); err != nil {
		return nil, err
	}
	a, tenant, err := agentSvc.GetActiveAgent(req.Username, cs.ScopeMerchant)
	if err != nil {
		s.recordFail(ctx, cs.ScopeMerchant, identity)
		if errors.Is(err, ErrAccountNotFound) || errors.Is(err, ErrPasswordFail) {
			return nil, ErrPasswordFail
		}
		return nil, err
	}
	if req.TenantCode != "" && tenant.Code != req.TenantCode {
		s.recordFail(ctx, cs.ScopeMerchant, identity)
		return nil, ErrTenantMismatch
	}
	if err = CheckPassword(a.Password, req.Password); err != nil {
		s.recordFail(ctx, cs.ScopeMerchant, identity)
		return nil, err
	}
	smsIdentity := fmt.Sprintf("%s:%d", cs.ScopeMerchant, a.ID)
	if err = smsSvc.Verify(ctx, cs.ScopeMerchant, smsIdentity, req.SmsCode); err != nil {
		s.recordFail(ctx, cs.ScopeMerchant, identity)
		return nil, err
	}
	s.clearFail(ctx, cs.ScopeMerchant, identity)

	token, expires, err := cstoken.CreateToken(cs.ScopeMerchant, a.ID, a.TenantID, a.Username, a.EmployeeNo, 0)
	if err != nil {
		return nil, err
	}
	return &csRes.MerchantLoginRes{
		CsTokenRes: csRes.CsTokenRes{Token: token, Expires: expires},
		Tenant:     &csRes.TenantInfo{ID: tenant.ID, Code: tenant.Code, Name: tenant.Name},
		Admin:      toAgentInfo(a),
	}, nil
}

// AgentLogin 客服工作台登录: 账密 + 图形验证码 + 短信验证码
func (s *AuthService) AgentLogin(ctx context.Context, req csReq.MfaLoginReq) (*csRes.AgentLoginRes, error) {
	identity := req.Username
	if err := s.checkLock(ctx, cs.ScopeAgent, identity); err != nil {
		return nil, err
	}
	if err := captchaSvc.Verify(req.CaptchaId, req.Captcha); err != nil {
		return nil, err
	}
	a, tenant, err := agentSvc.GetActiveAgent(req.Username, cs.ScopeAgent)
	if err != nil {
		s.recordFail(ctx, cs.ScopeAgent, identity)
		if errors.Is(err, ErrAccountNotFound) {
			return nil, ErrPasswordFail
		}
		return nil, err
	}
	// B2B 多渠道: 工作台链接携带的商户标识与账号归属必须一致(登录环节独立校验, 不依赖短信环节)
	if req.TenantCode != "" && tenant.Code != req.TenantCode {
		return nil, errors.New("该客服账号不属于当前商户渠道, 请确认接入链接")
	}
	if err = CheckPassword(a.Password, req.Password); err != nil {
		s.recordFail(ctx, cs.ScopeAgent, identity)
		return nil, err
	}
	smsIdentity := fmt.Sprintf("%s:%d", cs.ScopeAgent, a.ID)
	if err = smsSvc.Verify(ctx, cs.ScopeAgent, smsIdentity, req.SmsCode); err != nil {
		s.recordFail(ctx, cs.ScopeAgent, identity)
		return nil, err
	}
	s.clearFail(ctx, cs.ScopeAgent, identity)

	token, expires, err := cstoken.CreateToken(cs.ScopeAgent, a.ID, a.TenantID, a.Username, a.EmployeeNo, 0)
	if err != nil {
		return nil, err
	}
	return &csRes.AgentLoginRes{
		CsTokenRes: csRes.CsTokenRes{Token: token, Expires: expires},
		Tenant:     &csRes.TenantInfo{ID: tenant.ID, Code: tenant.Code, Name: tenant.Name},
		Agent:      toAgentInfo(a),
	}, nil
}

// MemberLogin 用户端 H5 登录: 仅账密; 支持接入链接携带的商户标识校验账号归属
func (s *AuthService) MemberLogin(ctx context.Context, req csReq.MemberLoginReq) (*csRes.MemberLoginRes, error) {
	m, err := memberSvc.GetActiveByUsername(req.Username)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return nil, ErrPasswordFail
		}
		return nil, err
	}
	if err = CheckPassword(m.Password, req.Password); err != nil {
		return nil, err
	}
	// B2B 多商户: 商户停用则其访客一律停止服务
	tenant, err := tenantSvc.GetActiveByID(m.TenantID)
	if err != nil {
		return nil, err
	}
	// 接入链接商户标识与账号归属租户必须一致, 防止跨商户串入
	if req.TenantCode != "" && tenant.Code != req.TenantCode {
		return nil, errors.New("该访客账号不属于当前商户渠道, 请确认接入链接")
	}
	token, expires, err := cstoken.CreateToken(cs.ScopeMember, m.ID, m.TenantID, m.Username, "", 0)
	if err != nil {
		return nil, err
	}
	return &csRes.MemberLoginRes{
		CsTokenRes: csRes.CsTokenRes{Token: token, Expires: expires},
		Tenant:     &csRes.TenantInfo{ID: tenant.ID, Code: tenant.Code, Name: tenant.Name},
		Member: &csRes.MemberInfo{
			ID: m.ID, TenantID: m.TenantID, Username: m.Username,
			NickName: m.NickName, Source: m.Source,
		},
	}, nil
}

func toAgentInfo(a *cs.CsAgent) *csRes.AgentInfo {
	return &csRes.AgentInfo{
		ID: a.ID, TenantID: a.TenantID, Username: a.Username,
		EmployeeNo: a.EmployeeNo, Role: a.Role, MaxActive: a.MaxActive,
		Phone: maskPhone(a.Phone),
	}
}
