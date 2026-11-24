package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/flipped-aurora/gin-vue-admin/server/core"
	"github.com/flipped-aurora/gin-vue-admin/server/global"
	cs "github.com/flipped-aurora/gin-vue-admin/server/model/cs"
	cssvc "github.com/flipped-aurora/gin-vue-admin/server/service/cs"
	"github.com/flipped-aurora/gin-vue-admin/server/utils/cstoken"
	"github.com/gorilla/websocket"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
)

// ws-gateway: 独立 WebSocket 推送网关。
// 职责: 握手鉴权(cstoken, 仅 agent/member 端) → 会话订阅(服务端 CanAccess 授权) →
//       消费 Kafka(cg-dispatch) 按会话广播新消息 → 心跳保活 → 订阅时按 lastSeq 补拉缺口。
// 设计: 与主 API 服务解耦, 只做"下行推送 + 订阅控制"; 发送/历史仍走 REST(热路径在 server)。

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMsgSize     = 8 * 1024
	singleConvBuff = 64
)

type client struct {
	gateway  *Gateway
	conn     *websocket.Conn
	send     chan []byte
	once     sync.Once
	scope    string
	accountID uint
	tenantID  uint
	convs     map[uint]bool // 已订阅会话
	tenantSub bool          // 是否已订阅租户级事件通道(仅 agent)
	mu       sync.Mutex
}

// Gateway 维护 convID → 订阅者集合, 另维护 tenantID → agent 连接集合(租户级提醒通道)
type Gateway struct {
	log    *zap.Logger
	upgrader websocket.Upgrader
	mu     sync.RWMutex
	hub    map[uint]map[*client]bool
	agents map[uint]map[*client]bool
}

func newGateway() *Gateway {
	return &Gateway{
		log: global.GVA_LOG,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  singleConvBuff,
			WriteBufferSize: singleConvBuff,
			// 跨域由部署层(nginx)收敛, 网关按 Origin 放行(开发)
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		hub:    make(map[uint]map[*client]bool),
		agents: make(map[uint]map[*client]bool),
	}
}

func (g *Gateway) addClient(convID uint, c *client) {
	g.mu.Lock()
	defer g.mu.Unlock()
	set := g.hub[convID]
	if set == nil {
		set = make(map[*client]bool)
		g.hub[convID] = set
	}
	set[c] = true
	c.mu.Lock()
	c.convs[convID] = true
	c.mu.Unlock()
}

func (g *Gateway) removeClient(convID uint, c *client) {
	g.mu.Lock()
	set := g.hub[convID]
	if set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(g.hub, convID)
		}
	}
	g.mu.Unlock()
	c.mu.Lock()
	delete(c.convs, convID)
	c.mu.Unlock()
}

func (g *Gateway) dropAll(c *client) {
	c.mu.Lock()
	ids := make([]uint, 0, len(c.convs))
	for id := range c.convs {
		ids = append(ids, id)
	}
	c.mu.Unlock()
	for _, id := range ids {
		g.removeClient(id, c)
	}
	// 从租户 agent 连接池摘除
	if c.scope == cs.ScopeAgent {
		g.mu.Lock()
		if set := g.agents[c.tenantID]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(g.agents, c.tenantID)
			}
		}
		g.mu.Unlock()
	}
	c.once.Do(func() { close(c.send) })
}

// broadcast 向订阅了 convID 的所有连接投递(非阻塞, 慢客户端丢弃由其补拉兜底)
func (g *Gateway) broadcast(convID uint, payload []byte) {
	g.mu.RLock()
	set := g.hub[convID]
	targets := make([]*client, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	g.mu.RUnlock()
	for _, c := range targets {
		select {
		case c.send <- payload:
		default:
			g.log.Warn("ws send buffer full, client will backfill", zap.Uint("convID", convID), zap.Uint("accountId", c.accountID))
		}
	}
}

// broadcastAgents 向租户内已订阅事件通道且未订阅 skipConv 的客服连接投递
// (skipConv 传会话 ID 避免同一连接经会话通道重复收到; 无需过滤时传 0)
func (g *Gateway) broadcastAgents(tenantID uint, payload []byte, skipConv uint) {
	g.mu.RLock()
	set := g.agents[tenantID]
	targets := make([]*client, 0, len(set))
	for c := range set {
		c.mu.Lock()
		sub := c.tenantSub
		_, inConv := c.convs[skipConv]
		c.mu.Unlock()
		if sub && !inConv {
			targets = append(targets, c)
		}
	}
	g.mu.RUnlock()
	for _, c := range targets {
		select {
		case c.send <- payload:
		default:
		}
	}
}

// broadcastAgent 定向投递给指定当值客服的连接(同租户其余客服不打扰)
func (g *Gateway) broadcastAgent(tenantID, accountID uint, payload []byte, skipConv uint) {
	g.mu.RLock()
	set := g.agents[tenantID]
	targets := make([]*client, 0, 1)
	for c := range set {
		c.mu.Lock()
		sub := c.tenantSub && c.accountID == accountID
		_, inConv := c.convs[skipConv]
		c.mu.Unlock()
		if sub && !inConv {
			targets = append(targets, c)
		}
	}
	g.mu.RUnlock()
	for _, c := range targets {
		select {
		case c.send <- payload:
		default:
		}
	}
}

// ---- 客户端协议 ----

type inMsg struct {
	Action         string `json:"action"` // subscribe|unsubscribe|subscribeTenant|pong
	ConversationID uint   `json:"conversationId"`
	LastSeq        int64  `json:"lastSeq"`
}

type outMsg struct {
	Type string      `json:"type"` // subscribed|message|error|pong|sync|tenant_subscribed|conv_event|tenant_message
	Body interface{} `json:"body,omitempty"`
	Err  string      `json:"error,omitempty"`
}

func (g *Gateway) handleWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("x-token")
	}
	claims, err := cstoken.ParseToken(token)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// 仅 agent / member 端可建立推送连接
	if claims.Scope != cs.ScopeAgent && claims.Scope != cs.ScopeMember {
		http.Error(w, "scope not allowed", http.StatusForbidden)
		return
	}
	conn, err := g.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{
		gateway:   g,
		conn:      conn,
		send:      make(chan []byte, singleConvBuff),
		scope:     claims.Scope,
		accountID: claims.AccountID,
		tenantID:  claims.TenantID,
		convs:     make(map[uint]bool),
	}
	if c.scope == cs.ScopeAgent {
		g.mu.Lock()
		set := g.agents[c.tenantID]
		if set == nil {
			set = make(map[*client]bool)
			g.agents[c.tenantID] = set
		}
		set[c] = true
		g.mu.Unlock()
	}
	go c.writePump()
	go c.readPump()
}

func (c *client) readPump() {
	defer func() {
		c.gateway.dropAll(c)
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMsgSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		var m inMsg
		if err := c.conn.ReadJSON(&m); err != nil {
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		switch m.Action {
		case "subscribe":
			c.handleSubscribe(m)
		case "unsubscribe":
			c.gateway.removeClient(m.ConversationID, c)
		case "subscribeTenant":
			// 租户级事件通道: 仅客服端可订, 接收会话生命周期事件与其他会话新消息提醒
			if c.scope != cs.ScopeAgent {
				c.trySend(outMsg{Type: "error", Err: "仅客服端可订阅租户通道"})
				return
			}
			c.mu.Lock()
			c.tenantSub = true
			c.mu.Unlock()
			c.trySend(outMsg{Type: "tenant_subscribed"})
		case "ping":
			c.trySend(outMsg{Type: "pong"})
		}
	}
}

func (c *client) handleSubscribe(m inMsg) {
	// 服务端授权: 不信任客户端会话ID, 校验成员关系(防越权订阅他人会话)
	conv, allowed := (&cssvc.ConversationService{}).CanAccess(c.tenantID, c.scope, c.accountID, m.ConversationID)
	if !allowed || conv == nil {
		c.trySend(outMsg{Type: "error", Err: "无权订阅该会话"})
		return
	}
	c.gateway.addClient(m.ConversationID, c)
	c.trySend(outMsg{Type: "subscribed", Body: map[string]uint{"conversationId": m.ConversationID}})
	// 补拉: 下发 lastSeq 之后已落库消息填补断线/首订缺口(lastSeq=0 即全量补拉,
	// 覆盖欢迎语等在 subscribe 送达前已广播的消息; 客户端按 seq 去重无副作用)
	go c.backfill(m.ConversationID, m.LastSeq)
}

func (c *client) backfill(convID uint, lastSeq int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := cssvc.QueryMessages(ctx, convID, 0, lastSeq, 200, time.Time{})
	if err != nil {
		c.gateway.log.Warn("ws backfill failed", zap.Uint("convID", convID), zap.Error(err))
		return
	}
	for i := range list {
		c.trySend(outMsg{Type: "sync", Body: list[i]})
	}
}

func (c *client) trySend(m outMsg) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	select {
	case c.send <- b:
	default:
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// dispatchLoop 消费 Kafka 推送组: 消息按会话广播; 会话事件另走租户级客服通道(待接入/状态变化)
func (g *Gateway) dispatchLoop(ctx context.Context, cli *kgo.Client, eventTopic string) {
	for {
		if ctx.Err() != nil {
			return
		}
		fetches := cli.PollRecords(ctx, 200)
		if fetches.Empty() {
			continue
		}
		var ok []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			if r.Topic == eventTopic {
				var ev cssvc.ConvEvent
				if err := json.Unmarshal(r.Value, &ev); err != nil {
					g.log.Warn("bad conv event dropped", zap.Error(err))
					ok = append(ok, r)
					return
				}
				payload, _ := json.Marshal(outMsg{Type: "conv_event", Body: ev})
				// 会话内成员(h5/在场客服) + 同租户其他客服(列表/待接入数刷新)
				g.broadcast(ev.ConversationID, payload)
				g.broadcastAgents(ev.TenantID, payload, ev.ConversationID)
				ok = append(ok, r)
				return
			}
			var ev cssvc.MessageEvent
			if err := json.Unmarshal(r.Value, &ev); err != nil {
				ok = append(ok, r)
				return
			}
			payload, _ := json.Marshal(outMsg{Type: "message", Body: ev})
			g.broadcast(ev.ConversationID, payload)
			// 客服未读提醒: 仅访客消息触发; 已有当值者只提醒当值客服, 待接入会话提醒全租户鼓励认领
			if ev.SenderType == cs.ScopeMember {
				notify, _ := json.Marshal(outMsg{Type: "tenant_message", Body: ev})
				if ev.AssigneeAgentID > 0 {
					g.broadcastAgent(ev.TenantID, ev.AssigneeAgentID, notify, ev.ConversationID)
				} else {
					g.broadcastAgents(ev.TenantID, notify, ev.ConversationID)
				}
			}
			ok = append(ok, r)
		})
		if len(ok) > 0 {
			cli.MarkCommitRecords(ok...)
		}
	}
}

func main() {
	core.WorkerInit()
	g := newGateway()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	group := global.GVA_CONFIG.Kafka.DispatchGroup
	if group == "" {
		group = "cg-dispatch"
	}
	eventTopic := cssvc.KafkaEventTopic()
	cli, err := cssvc.NewConsumerTopics(group, cssvc.KafkaMsgTopic(), eventTopic)
	if err != nil {
		g.log.Fatal("ws-gateway consumer init failed", zap.Error(err))
	}
	defer cli.Close()
	go g.dispatchLoop(ctx, cli, eventTopic)

	addr := global.GVA_CONFIG.Kafka.WsAddr
	if addr == "" {
		addr = ":8889"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", g.handleWS)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		g.log.Info("ws-gateway listening", zap.String("addr", addr), zap.String("group", group))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			g.log.Fatal("ws-gateway server failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	g.log.Info("ws-gateway stopped")
}
