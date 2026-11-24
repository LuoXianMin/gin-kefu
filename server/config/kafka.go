package config

// Kafka 客服系统消息链路配置
// 开发 Compose 环境为单 broker KRaft；生产需 RF=3/min.insync=2，见方案 §10.3
type Kafka struct {
	Enabled       bool     `mapstructure:"enabled" json:"enabled" yaml:"enabled"`                       // 关闭时消息链路走同步降级(仅PG水位,不推荐生产)
	Brokers       []string `mapstructure:"brokers" json:"brokers" yaml:"brokers"`                       // broker 地址列表
	Topic         string   `mapstructure:"topic" json:"topic" yaml:"topic"`                             // 消息主题, 默认 im.message
	EventTopic    string   `mapstructure:"event-topic" json:"event-topic" yaml:"event-topic"`           // 会话生命周期事件主题, 默认 im.conversation.event
	StorageGroup  string   `mapstructure:"storage-group" json:"storage-group" yaml:"storage-group"`     // 落库消费者组, 默认 cg-storage
	DispatchGroup string   `mapstructure:"dispatch-group" json:"dispatch-group" yaml:"dispatch-group"`  // 推送消费者组(WS Gateway), 默认 cg-dispatch
	WsAddr        string   `mapstructure:"ws-addr" json:"ws-addr" yaml:"ws-addr"`                        // WS Gateway 监听地址, 默认 :8889
}
