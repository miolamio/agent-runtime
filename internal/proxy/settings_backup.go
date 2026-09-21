package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
)

// This journal is shared with connect-proxy.sh and connect-proxy.ps1. Each
// entry records only top-level fields touched by airun. Disconnect performs a
// three-way undo so edits made by Claude Code or the user survive.
const backupKey = "_airunBackup"

type settingsBackup struct {
	Version int            `json:"version"`
	Created bool           `json:"created"`
	Before  map[string]any `json:"before"`
	After   map[string]any `json:"after"`
}

func cloneSettings(value map[string]any) map[string]any {
	data, _ := json.Marshal(value)
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	return result
}

func readBackup(settings map[string]any) (*settingsBackup, error) {
	value, exists := settings[backupKey]
	if !exists {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var backup settingsBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return nil, err
	}
	if backup.Version != 1 || backup.Before == nil || backup.After == nil {
		return nil, fmt.Errorf("unsupported or damaged airun settings backup")
	}
	return &backup, nil
}

func containsValue(items []any, value any) bool {
	for _, item := range items {
		if reflect.DeepEqual(item, value) {
			return true
		}
	}
	return false
}

func undoSettings(current, before, after map[string]any) map[string]any {
	result := cloneSettings(current)
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	for key := range keys {
		old, hadOld := before[key]
		written, hadWritten := after[key]
		value, exists := result[key]
		if hadOld == hadWritten && reflect.DeepEqual(old, written) {
			continue
		}
		if exists == hadWritten && reflect.DeepEqual(value, written) {
			if hadOld {
				result[key] = old
			} else {
				delete(result, key)
			}
			continue
		}
		curMap, curOK := value.(map[string]any)
		afterMap, afterOK := written.(map[string]any)
		if curOK && afterOK {
			beforeMap, _ := old.(map[string]any)
			restored := undoSettings(curMap, beforeMap, afterMap)
			if !hadOld && len(restored) == 0 {
				delete(result, key)
			} else {
				result[key] = restored
			}
			continue
		}
		curArray, curOK := value.([]any)
		afterArray, afterOK := written.([]any)
		if curOK && afterOK {
			beforeArray, _ := old.([]any)
			restored := []any{}
			for _, item := range curArray {
				if !containsValue(afterArray, item) || containsValue(beforeArray, item) {
					restored = append(restored, item)
				}
			}
			if !hadOld && len(restored) == 0 {
				delete(result, key)
			} else {
				result[key] = restored
			}
		}
	}
	return result
}

func updateManagedSettings(path string, update func(map[string]any) error) error {
	settings, err := readSettings(path)
	if err != nil {
		return err
	}
	backup, err := readBackup(settings)
	if err != nil {
		return err
	}
	_, statErr := os.Stat(path)
	created := os.IsNotExist(statErr)
	before := cloneSettings(settings)
	if backup != nil {
		created = backup.Created
		delete(settings, backupKey)
		delete(settings, "_airunManaged")
		before = undoSettings(settings, backup.Before, backup.After)
	}
	if err := update(settings); err != nil {
		return err
	}
	after := cloneSettings(settings)
	delete(after, backupKey)
	delete(after, "_airunManaged")
	// Keep only changed fields, leaving large unrelated project data out.
	oldFields, newFields := map[string]any{}, map[string]any{}
	for key, value := range after {
		old, exists := before[key]
		if !exists || !reflect.DeepEqual(old, value) {
			newFields[key] = value
			if exists {
				oldFields[key] = old
			}
		}
	}
	settings[backupKey] = settingsBackup{Version: 1, Created: created, Before: oldFields, After: newFields}
	settings["_airunManaged"] = true
	return writeSettings(path, settings)
}

func cleanManagedSettings(path string) (bool, error) {
	settings, err := readSettings(path)
	if err != nil {
		return false, err
	}
	backup, err := readBackup(settings)
	if err != nil {
		return false, err
	}
	// Old boolean markers do not establish ownership of any user data.
	if backup == nil {
		return false, nil
	}
	delete(settings, backupKey)
	delete(settings, "_airunManaged")
	restored := undoSettings(settings, backup.Before, backup.After)
	if backup.Created && len(restored) == 0 {
		return true, os.Remove(path)
	}
	return true, writeSettings(path, restored)
}
