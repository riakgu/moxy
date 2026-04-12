package model

type LogEntry struct {
	Time      int64             `json:"time"`
	Level     string            `json:"level"`
	Message   string            `json:"msg"`
	Component string            `json:"component,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}
