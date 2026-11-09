package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// configLeaves lists every setting of config.Config: a dotted key per field
// that is not a section, with whether it is a list of entries (a slice of
// structs, or a map).
func configLeaves() map[string]bool {
	leaves := map[string]bool{}
	var walk func(prefix string, typ reflect.Type)
	walk = func(prefix string, typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			tag := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			key := tag
			if prefix != "" {
				key = prefix + "." + tag
			}
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			switch {
			case ft.Kind() == reflect.Struct:
				walk(key, ft)
			case ft.Kind() == reflect.Map, ft.Kind() == reflect.Slice && ft.Elem().Kind() != reflect.String:
				leaves[key] = true
			default:
				leaves[key] = false
			}
		}
	}
	walk("", reflect.TypeOf(Config{}))
	return leaves
}

// Every setting of config.yaml is registered, file-only, or a list of
// entries.
func TestEverySettingIsRegisteredOrFileOnly(t *testing.T) {
	for key, isList := range configLeaves() {
		if IsFileOnly(key) {
			continue
		}
		if _, ok := LookupSetting(key); ok {
			continue
		}
		if isList && slices.Contains(EntryLists(), key) {
			continue
		}
		t.Errorf("%s is neither registered nor file-only: register it in config/settings.go, or add it to fileOnly with a reason", key)
	}
}

func TestASecretSettingTakesOnlyASecretsName(t *testing.T) {
	s, _ := LookupSetting("jira.api_token")
	if !s.Secret {
		t.Fatal("jira.api_token is not marked secret")
	}
	for _, ok := range []string{`"legato://jira-token"`, `"op://Work/Jira/token"`, `""`} {
		if _, err := DecodeSetting(s, []byte(ok)); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	if _, err := DecodeSetting(s, []byte(`"ATATT3xFfGF0abc"`)); err == nil || !strings.Contains(err.Error(), "never the value") {
		t.Errorf("a token itself was taken: %v", err)
	}
}
