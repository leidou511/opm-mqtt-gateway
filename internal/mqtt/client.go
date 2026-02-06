package mqtt

import (
	"encoding/json"
	"log"
	"opm-mqtt-gateway/internal/config"
	"opm-mqtt-gateway/internal/models"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type MQTTClient struct {
	client MQTT.Client
	config *config.MQTTConfig
}

func NewMQTTClient(cfg *config.MQTTConfig) *MQTTClient {
	opts := MQTT.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID(cfg.ClientID)
	opts.SetUsername(cfg.Username)
	opts.SetPassword(cfg.Password)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(10 * time.Second)
	opts.SetKeepAlive(60 * time.Second)

	opts.OnConnect = func(client MQTT.Client) {
		log.Printf("MQTT连接成功: %s", cfg.Broker)
	}

	opts.OnConnectionLost = func(client MQTT.Client, err error) {
		log.Printf("MQTT连接丢失: %v", err)
	}

	return &MQTTClient{
		config: cfg,
		client: MQTT.NewClient(opts),
	}
}

func (mc *MQTTClient) Connect() error {
	if token := mc.client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (mc *MQTTClient) IsConnected() bool {
	return mc.client.IsConnected()
}

func (mc *MQTTClient) PublishResult(result *models.UrineTestResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	token := mc.client.Publish(mc.config.Topic, mc.config.QOS, mc.config.Retain, data)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	log.Printf("MQTT发布成功: topic=%s, 样本号=%s", mc.config.Topic, result.SampleID)
	return nil
}

func (mc *MQTTClient) Disconnect() {
	mc.client.Disconnect(250)
	log.Println("MQTT连接已断开")
}
