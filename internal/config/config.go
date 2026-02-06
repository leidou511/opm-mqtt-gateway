package config

import (
	"gopkg.in/yaml.v3"

	"io/ioutil"
	"log"
	"os"
)

type Config struct {
	App     AppConfig     `yaml:"app"`
	Serial  SerialConfig  `yaml:"serial"`
	MQTT    MQTTConfig    `yaml:"mqtt"`
	Logging LoggingConfig `yaml:"logging"`
}

type AppConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

type SerialConfig struct {
	Port     string `yaml:"port"`
	BaudRate int    `yaml:"baud_rate"`
	DataBits int    `yaml:"data_bits"`
	StopBits int    `yaml:"stop_bits"`
	Parity   string `yaml:"parity"`
}

type MQTTConfig struct {
	Broker   string `yaml:"broker"`
	ClientID string `yaml:"client_id"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Topic    string `yaml:"topic"`
	QOS      byte   `yaml:"qos"`
	Retain   bool   `yaml:"retain"`
}

type LoggingConfig struct {
	Level    string `yaml:"level"`
	FilePath string `yaml:"file_path"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func InitLogging(config *LoggingConfig) error {
	if err := os.MkdirAll("./logs", 0755); err != nil {
		return err
	}

	file, err := os.OpenFile(config.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}

	log.SetOutput(file)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	return nil
}
