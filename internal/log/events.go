package log

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
)

func Append(root string, event string, id string, actor string, data map[string]any) error {
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(root, "logs", "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	e := model.Event{Time: time.Now(), Event: event, ID: id, Actor: actor, Data: data}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}
