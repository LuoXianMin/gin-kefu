package cs

import (
	"errors"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"github.com/mojocn/base64Captcha"
)

// captchaStore 复用框架的进程内验证码存储(与 /base/captcha 同一单例)
// 多服务器部署时应替换为 Redis Store(见框架 sys_captcha.go 注释)
var captchaStore = base64Captcha.DefaultMemStore

// CaptchaService 图形验证码生成与校验
type CaptchaService struct{}

// Generate 生成数字图形验证码, 返回 (captchaId, base64图片, 答案)
// 答案仅非 public 环境回显给接口(开发/测试用, 与 sms.dev-return-code 同风格); 生产 env=public 下答案不出域
func (s *CaptchaService) Generate() (id, b64s, answer string, err error) {
	cfg := global.GVA_CONFIG.Captcha
	height := cfg.ImgHeight
	width := cfg.ImgWidth
	length := cfg.KeyLong
	if height <= 0 {
		height = 60
	}
	if width <= 0 {
		width = 180
	}
	if length <= 0 {
		length = 4
	}
	driver := base64Captcha.NewDriverDigit(height, width, length, 0.7, 80)
	cp := base64Captcha.NewCaptcha(driver, captchaStore)
	id, b64s, answer, err = cp.Generate()
	if err == nil && global.GVA_CONFIG.System.Env == "public" {
		answer = "" // 生产不回显
	}
	return
}

// Verify 校验图形验证码(一次性)
func (s *CaptchaService) Verify(id, answer string) error {
	if id == "" || answer == "" {
		return errors.New("图形验证码不能为空")
	}
	if !captchaStore.Verify(id, answer, true) {
		return errors.New("图形验证码错误或已过期")
	}
	return nil
}
