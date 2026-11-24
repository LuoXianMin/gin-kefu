package request

// ConversationCreateReq 用户端创建/恢复会话
type ConversationCreateReq struct {
	Title  string `json:"title"`
	Source string `json:"source"`
}

// TransferReq 转接会话
type TransferReq struct {
	ToAgentID uint   `json:"toAgentId" binding:"required"`
	Reason    string `json:"reason"`
}

// CloseReq 结束会话
type CloseReq struct {
	Reason string `json:"reason"`
}

// HistoryQueryReq 历史消息查询(客户端仅可传分页, 时间窗口由服务端裁决)
type HistoryQueryReq struct {
	BeforeSeq int64 `form:"beforeSeq"` // 向更早翻页的游标(不含)
	AfterSeq  int64 `form:"afterSeq"`  // 断线补拉: 拉取大于该 seq 的消息
	Limit     int   `form:"limit"`
}

// SendMsgReq 发送消息(热路径)
type SendMsgReq struct {
	ClientMsgID string `json:"clientMsgId" binding:"required"` // 幂等键, 重试必须复用
	Type        string `json:"type" binding:"required"`        // text/image/file...
	Body        string `json:"body" binding:"required"`
}

// ReadReportReq 已读水位上报
type ReadReportReq struct {
	LastReadSeq int64 `json:"lastReadSeq" binding:"required"`
}
