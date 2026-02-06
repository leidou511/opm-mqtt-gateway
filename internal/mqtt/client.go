package mqtt

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"opm-mqtt-gateway/internal/config"
	"opm-mqtt-gateway/internal/models"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

// Client MQTT客户端实例（贴合医用数据要求，基于paho.mqtt v1.5.1实现）
type Client struct {
	client      MQTT.Client        // paho原生客户端
	cfg         *config.Config     // 全局配置
	ctx         context.Context    // 协程管理上下文
	cancel      context.CancelFunc // 协程取消函数
	mu          sync.Mutex         // 操作互斥锁（并发安全）
	isConnected bool               // MQTT连接状态
	topicData   string             // 检测数据发布主题（设备SN唯一）
	topicState  string             // 设备状态发布主题（遗嘱+主动上报）
}

// NewClient 新建MQTT客户端实例（初始化遗嘱+QoS1+重连协程）
func NewClient() (*Client, error) {
	cfg := config.GlobalConfig
	// 1. 初始化上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 2. 生成设备唯一发布主题
	topicData := fmt.Sprintf("%s/%s/data", cfg.MQTT.TopicPrefix, cfg.Device.DeviceID)
	topicState := cfg.MQTT.WillTopic

	// 3. paho.mqtt v1.5.1标准配置（核心：医用数据优化）
	opts := MQTT.NewClientOptions()
	opts.AddBroker(cfg.MQTT.Broker)
	opts.SetClientID(cfg.MQTT.ClientID)
	opts.SetUsername(cfg.MQTT.Username)
	opts.SetPassword(cfg.MQTT.Password)
	opts.SetCleanSession(true)
	opts.SetKeepAlive(time.Duration(cfg.MQTT.KeepAlive) * time.Second)
	opts.SetAutoReconnect(false) // 关闭原生重连，自定义指数退避（工业现场更友好）
	opts.SetMaxReconnectInterval(time.Duration(cfg.MQTT.ReconnectInt*10) * time.Second)

	// 4. 设置遗嘱消息（核心：设备异常离线时，平台自动接收offline）
	opts.SetWill(topicState, cfg.MQTT.WillMsg, uint8(cfg.MQTT.WillQoS), cfg.MQTT.WillRetain)

	// 5. 连接成功回调：主动上报online状态（平台实时感知设备上线）
	opts.SetOnConnectHandler(func(c MQTT.Client) {
		log.Printf("[INFO] [mqtt] 连接成功，服务端：%s，客户端ID：%s", cfg.MQTT.Broker, cfg.MQTT.ClientID)
		_ = rptOnlineState(c, topicState, cfg)
	})

	// 6. 连接丢失回调：记录错误，触发重连协程
	opts.SetConnectionLostHandler(func(c MQTT.Client, err error) {
		log.Printf("[ERROR] [mqtt] 连接丢失：%v", err)
	})

	// 7. 新建paho客户端
	client := MQTT.NewClient(opts)

	// 8. 新建自定义客户端实例
	m := &Client{
		client:      client,
		cfg:         cfg,
		ctx:         ctx,
		cancel:      cancel,
		topicData:   topicData,
		topicState:  topicState,
		isConnected: false,
	}

	// 9. 连接MQTT服务端（带基础重试）
	if err := m.connectWithRetry(); err != nil {
		return nil, fmt.Errorf("连接失败：%w", err)
	}

	// 10. 启动指数退避重连协程（7*24运行，网络波动自动恢复）
	go m.reconnectLoop()

	return m, nil
}

// connectWithRetry MQTT连接（带基础重试，避免网络偶发失败）
func (m *Client) connectWithRetry() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	retryCnt := 3
	retryInt := time.Duration(m.cfg.MQTT.ReconnectInt) * time.Second
	for i := 1; i <= retryCnt; i++ {
		if token := m.client.Connect(); token.Wait() && token.Error() != nil {
			log.Printf("[ERROR] [mqtt] 重试%d/%d：%v", i, retryCnt, token.Error())
			time.Sleep(retryInt)
			continue
		}
		m.isConnected = true
		return nil
	}
	return fmt.Errorf("重试%d次后失败", retryCnt)
}

// reconnectLoop 核心：指数退避重连（工业现场网络波动适配）
// 规则：基础间隔2s → 4s → 8s → 最大20s，重连成功后重置为2s
func (m *Client) reconnectLoop() {
	baseInt := time.Duration(m.cfg.MQTT.ReconnectInt) * time.Second
	maxInt := baseInt * 10
	curInt := baseInt

	for {
		select {
		case <-m.ctx.Done():
			log.Printf("[INFO] [mqtt] 重连协程正常退出")
			return
		default:
			m.mu.Lock()
			connected := m.isConnected
			m.mu.Unlock()

			if !connected {
				log.Printf("[WARN] [mqtt] 开始重连，当前间隔：%v", curInt)
				if err := m.connectWithRetry(); err != nil {
					curInt = min(curInt*2, maxInt) // 指数退避
					time.Sleep(curInt)
					continue
				}
				// 重连成功，重置间隔，更新状态
				curInt = baseInt
				m.mu.Lock()
				m.isConnected = true
				m.mu.Unlock()
			}
			time.Sleep(baseInt) // 连接正常时，间隔检查状态
		}
	}
}

// rptOnlineState 连接成功后，主动上报设备online状态（平台感知）
func rptOnlineState(client MQTT.Client, topic string, cfg *config.Config) error {
	// 构建状态MQTT消息
	stateMsg := models.NewMQTTMessage(
		cfg.Device.DeviceID,
		cfg.Device.Model,
		models.MQTTMsgTypeState,
		models.DeviceStateOnline,
	)
	jsonMsg, err := stateMsg.ToJSON()
	if err != nil {
		return fmt.Errorf("序列化失败：%w", err)
	}

	// 发布状态消息
	token := client.Publish(topic, uint8(cfg.MQTT.WillQoS), cfg.MQTT.WillRetain, jsonMsg)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("发布失败：%w", token.Error())
	}

	log.Printf("[INFO] [mqtt] 已上报设备在线状态，主题：%s，消息：%s", topic, string(jsonMsg))
	return nil
}

// Publish 核心发布方法（v1.5.1专属，无SetCallback，异步非阻塞，适配OPM-1560B）
func (c *Client) Publish(mqttMsg *models.MQTTMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. 前置强校验：从源头避免nil client/未连接/空token（核心兜底）
	if c.client == nil {
		err := errors.New("MQTT原生客户端未初始化")
		log.Printf("[ERROR] [mqtt] 设备[%s]发布失败：%v", c.cfg.Device.DeviceID, err)
		return err
	}
	if !c.isConnected || c.client.IsConnectionOpen() {
		err := errors.New("MQTT客户端未建立有效连接")
		log.Printf("[ERROR] [mqtt] 设备[%s]发布失败：%v", c.cfg.Device.DeviceID, err)
		return err
	}

	// 2. 标准化消息序列化（复用models层ToJSON方法，保证格式统一）
	payload, err := mqttMsg.ToJSON()
	if err != nil {
		log.Printf("[ERROR] [mqtt] 设备[%s]消息序列化失败：%v", c.cfg.Device.DeviceID, err)
		return err
	}

	// 3. 按消息类型生成标准化主题（data/state分离，适配物联网平台解析）
	var topic string
	switch mqttMsg.MsgType {
	case models.MQTTMsgTypeData:
		topic = c.cfg.MQTT.TopicPrefix + "/" + c.cfg.Device.DeviceID + "/data" // 检测数据主题
	case models.MQTTMsgTypeState:
		topic = c.cfg.MQTT.TopicPrefix + "/" + c.cfg.Device.DeviceID + "/state" // 设备状态主题
	default:
		err := errors.New("无效的MQTT消息类型，仅支持data/state")
		log.Printf("[ERROR] [mqtt] 设备[%s]发布失败：%v", c.cfg.Device.DeviceID, err)
		return err
	}

	// 4. 发布消息（固化QoS1，满足医用数据至少一次送达要求）
	// retained=false：非保留消息，贴合实时检测数据特性
	tk := c.client.Publish(topic, byte(c.cfg.MQTT.QoS), false, payload)

	// 5. 兜底nil token：即使前置校验，网络瞬断仍可能返回nil，直接报错
	if tk == nil {
		err := errors.New("Publish调用返回nil Token，客户端连接异常")
		log.Printf("[ERROR] [mqtt] 设备[%s]发布失败：%v | 主题：%s", c.cfg.Device.DeviceID, err, topic)
		return err
	}

	// 闭包携带设备ID/主题/QoS，保证日志信息完整，不阻塞串口数据采集协程
	go func(deviceID, topic string, qos byte) {
		// 等待发布结果（同步，仅在协程内阻塞，不影响主流程）
		if err := tk.Wait(); err == false {
			log.Printf("[ERROR] [mqtt] 设备[%s]MQTT消息发布失败 | 主题：%s | QoS：%d | 错误：%v", deviceID, topic, qos, err)
		} else {
			log.Printf("[INFO] [mqtt] 设备[%s]MQTT消息发布成功 | 主题：%s | QoS：%d | 消息长度：%d字节", deviceID, topic, qos, len(payload))
		}
	}(c.cfg.Device.DeviceID, topic, byte(c.cfg.MQTT.QoS))

	return nil
}

// Close 优雅关闭MQTT客户端：主动上报offline+断开连接+取消协程
func (m *Client) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil && m.isConnected {
		// 1. 主动上报offline状态（程序正常退出，平台精准感知）
		offlineMsg := models.NewMQTTMessage(
			m.cfg.Device.DeviceID,
			m.cfg.Device.Model,
			models.MQTTMsgTypeState,
			models.DeviceStateOffline,
		)
		if err := m.Publish(offlineMsg); err != nil {
			log.Printf("[WARN] [mqtt] 发布离线状态失败：%v", err)
		}

		// 2. 断开MQTT连接（paho标准方法，250ms等待消息发送完成）
		m.client.Disconnect(250)
		m.isConnected = false
		log.Printf("[INFO] [mqtt] 客户端已关闭，服务端：%s", m.cfg.MQTT.Broker)
	}

	// 3. 取消协程
	m.cancel()
}

// IsConnected 获取MQTT连接状态（供上游判断是否可发布数据）
func (m *Client) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isConnected
}

// min 工具方法：取两个时间间隔的最小值（指数退避用）
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
