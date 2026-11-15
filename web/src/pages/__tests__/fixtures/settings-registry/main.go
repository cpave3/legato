// Emits the actual configuration registry for the SettingsPage integration test.
package main

import (
	"encoding/json"
	"github.com/cpave3/legato/config"
	"os"
)

func main() {
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"settings": config.Settings(), "file_only": config.FileOnlySettings(),
		"lists": config.EntryListDefs(), "entry_lists": config.EntryLists(),
	}); err != nil {
		panic(err)
	}
}
