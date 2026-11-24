package cs

import (
	"context"
	"errors"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/global"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const msgColl = "cs_messages"

var ErrMongoUnavailable = errors.New("MongoDB 未启用或不可用")

func messagesCol() (*mongo.Collection, error) {
	if global.GVA_MONGO == nil {
		return nil, ErrMongoUnavailable
	}
	return global.GVA_MONGO.Database.Collection(msgColl).CloneCollection()
}

// EnsureMessageIndexes 建立消息查询与幂等相关索引
func EnsureMessageIndexes(ctx context.Context) error {
	coll, err := messagesCol()
	if err != nil {
		return err
	}
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "message_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "conversation_id", Value: 1}, {Key: "seq", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "conversation_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "conversation_id", Value: 1}, {Key: "sender_id", Value: 1}, {Key: "client_msg_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("idx_idem")},
	}
	_, err = coll.Indexes().CreateMany(ctx, indexes)
	return err
}

// SaveMessage 幂等落库(按 message_id upsert)。返回是否新插入。
func SaveMessage(ctx context.Context, ev *MessageEvent) (bool, error) {
	coll, err := messagesCol()
	if err != nil {
		return false, err
	}
	filter := bson.M{"message_id": ev.MessageID}
	update := bson.M{"$setOnInsert": ev}
	res, err := coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if err != nil {
		return false, err
	}
	return res.UpsertedCount > 0, nil
}

// MaxStoredSeq 已落库的最大 seq(水位恢复用)
func MaxStoredSeq(ctx context.Context, convID uint) (int64, error) {
	coll, err := messagesCol()
	if err != nil {
		return 0, err
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "seq", Value: -1}}).SetProjection(bson.M{"seq": 1})
	var doc struct {
		Seq int64 `bson:"seq"`
	}
	if err := coll.FindOne(ctx, bson.M{"conversation_id": convID}, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return 0, nil
		}
		return 0, err
	}
	return doc.Seq, nil
}

// QueryMessages 按会话与 seq 范围查询(升序), since 非零时仅返回 created_at>=since 的消息(用户侧7天窗口)
func QueryMessages(ctx context.Context, convID uint, beforeSeq, afterSeq int64, limit int, since time.Time) ([]MessageEvent, error) {
	coll, err := messagesCol()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	filter := bson.M{"conversation_id": convID}
	if beforeSeq > 0 {
		filter["seq"] = bson.M{"$lt": beforeSeq}
	}
	if afterSeq > 0 {
		if existing, ok := filter["seq"].(bson.M); ok {
			existing["$gt"] = afterSeq
		} else {
			filter["seq"] = bson.M{"$gt": afterSeq}
		}
	}
	if !since.IsZero() {
		filter["created_at"] = bson.M{"$gte": since.UTC()}
	}
	sortDir := -1
	if afterSeq > 0 {
		sortDir = 1 // 补拉按升序
	}
	opts := options.Find().SetSort(bson.D{{Key: "seq", Value: sortDir}}).SetLimit(int64(limit))
	cur, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []MessageEvent
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	// 统一按 seq 升序返回
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}
