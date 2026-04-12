package entity

type LogEntry struct {
	Time      int64
	Level     string
	Message   string
	Component string
	Attrs     map[string]string
}
