package response

import "time"

// FileRes 附件元数据(不含存储路径)
type FileRes struct {
	ID             uint      `json:"id"`
	ConversationID uint      `json:"conversationId"`
	FileName       string    `json:"fileName"`
	Size           int64     `json:"size"`
	MimeType       string    `json:"mimeType"`
	SHA256         string    `json:"sha256"`
	ScanStatus     string    `json:"scanStatus"`
	CreatedAt      time.Time `json:"createdAt"`
}

// FileURLRes 短时效下载链接
type FileURLRes struct {
	FileID   uint      `json:"fileId"`
	FileName string    `json:"fileName"`
	URL      string    `json:"url"`
	ExpireAt time.Time `json:"expireAt"`
}
