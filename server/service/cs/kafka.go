package cs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	producerOnce sync.Once
	producerCli  *kgo.Client
	producerErr  error
)

func kafkaBrokers() []string {
	b := global.GVA_CONFIG.Kafka.Brokers
	if len(b) == 0 {
		return []string{"gva-kafka:9092"}
	}
	return b
}

func kafkaTopic() string {
	if t := global.GVA_CONFIG.Kafka.Topic; t != "" {
		return t
	}
	return "im.message"
}

// KafkaMsgTopic 消息主题(导出供网关构造多 topic 消费者)
func KafkaMsgTopic() string { return kafkaTopic() }

// KafkaEventTopic 会话生命周期事件主题(供 ws-gateway 订阅, 导出给网关侧复用)
func KafkaEventTopic() string {
	if t := global.GVA_CONFIG.Kafka.EventTopic; t != "" {
		return t
	}
	return "im.conversation.event"
}

// getProducer 惰性构建单例 Producer: 幂等开启(默认), 要求 acks=all
func getProducer() (*kgo.Client, error) {
	producerOnce.Do(func() {
		if !global.GVA_CONFIG.Kafka.Enabled {
			producerErr = errors.New("kafka 未启用")
			return
		}
		cli, err := kgo.NewClient(
			kgo.SeedBrokers(kafkaBrokers()...),
			kgo.RequiredAcks(kgo.AllISRAcks()), // acks=all
			kgo.ProducerLinger(0),              // 立即发送, 降低 ACK 延迟
			kgo.RequestTimeoutOverhead(15*time.Second),
			kgo.DisableIdempotentWrite(), // 单机开发 broker 可能不支持幂等事务; 生产应移除此行启用幂等 Producer
		)
		if err != nil {
			producerErr = err
			return
		}
		// 探测连接
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cli.Ping(ctx); err != nil {
			producerErr = err
			return
		}
		producerCli = cli
	})
	return producerCli, producerErr
}

// produceMessage 同步生产, 以 conversationID 为 key, 成功返回 nil
func produceMessage(ctx context.Context, key string, value []byte) error {
	cli, err := getProducer()
	if err != nil {
		return err
	}
	rec := &kgo.Record{Topic: kafkaTopic(), Key: []byte(key), Value: value}
	result := cli.ProduceSync(ctx, rec)
	return result.FirstErr()
}

// produceConvEvent 同步生产会话事件到独立事件 topic
func produceConvEvent(ctx context.Context, ev *ConvEvent) error {
	cli, err := getProducer()
	if err != nil {
		return err
	}
	val, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	rec := &kgo.Record{Topic: KafkaEventTopic(), Key: []byte(fmt.Sprintf("%d", ev.ConversationID)), Value: val}
	result := cli.ProduceSync(ctx, rec)
	return result.FirstErr()
}

// NewConsumer 构建消费者组客户端(供 worker / gateway 复用)
func NewConsumer(group string) (*kgo.Client, error) {
	return NewConsumerTopics(group, kafkaTopic())
}

// NewConsumerTopics 订阅多个 topic 的消费者组客户端
func NewConsumerTopics(group string, topics ...string) (*kgo.Client, error) {
	return kgo.NewClient(
		kgo.SeedBrokers(kafkaBrokers()...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topics...),
		kgo.SessionTimeout(30*time.Second),
		kgo.AutoCommitMarks(),
		kgo.AutoCommitInterval(1000*time.Millisecond),
	)
}
