package common

import "encoding/json"

func ConfigArgument(paths []string) string {
	if len(paths) == 0 || paths[0] == "" {
		return ""
	}
	return " --config " + QuotePathForShell(paths[0])
}

// RemoveManagedEvent retires one subscription while retaining the user's other handlers.
func RemoveManagedEvent(events *OrderedObject, event, marker string) error {
	raw, ok := events.Get(event)
	if !ok {
		return nil
	}
	entries, err := HookEntries(raw)
	if err != nil {
		return err
	}
	kept := []json.RawMessage{}
	for _, entry := range entries {
		if stripped, keep := StripManagedHooks(entry, marker); keep {
			kept = append(kept, stripped)
		}
	}
	if len(kept) == 0 {
		events.Delete(event)
		return nil
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	events.Set(event, encoded)
	return nil
}
