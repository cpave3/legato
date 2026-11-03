package config

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The docs site's configuration page names every setting a person can
// write in config.yaml. A setting that is added here and not documented
// there fails this test.
const configurationPage = "../site/src/content/docs/reference/configuration.md"

func TestConfigurationReferenceNamesEverySetting(t *testing.T) {
	page, err := os.ReadFile(configurationPage)
	if err != nil {
		t.Fatal(err)
	}
	documented := documentedKeys(string(page))

	var missing []string
	for _, key := range configKeys(reflect.TypeOf(Config{}), "") {
		if !documented[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%s does not name these settings (write each as `key` in code):\n  %s",
			configurationPage, strings.Join(missing, "\n  "))
	}
}

func TestConfigKeysNameListItemsAndMapEntries(t *testing.T) {
	keys := strings.Join(configKeys(reflect.TypeOf(Config{}), ""), " ")
	for _, want := range []string{"jira.base_url", "board.columns[].name", "adapters.<>.launch_args", "adapters.<>.tiers.<>.description", "swarm.prompts"} {
		if !strings.Contains(" "+keys+" ", " "+want+" ") {
			t.Errorf("configKeys lacks %s", want)
		}
	}
	if strings.Contains(" "+keys+" ", " jira ") {
		t.Error("configKeys lists the block jira, which is not a setting of its own")
	}
}

// configKeys lists the settings of a config type as dotted paths: a list
// item is written name[], a map entry <>. A block (a struct, or a list or
// map of structs) is not listed itself; its settings are.
func configKeys(t reflect.Type, prefix string) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if !field.IsExported() || name == "-" || name == "" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if inner, path, ok := block(field.Type, path); ok {
			keys = append(keys, configKeys(inner, path)...)
			continue
		}
		keys = append(keys, path)
	}
	return keys
}

// block reports whether a field holds settings of its own, and the type
// and path of each.
func block(t reflect.Type, path string) (reflect.Type, string, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return t, path, true
	case reflect.Slice:
		if inner, p, ok := block(t.Elem(), path+"[]"); ok {
			return inner, p, true
		}
	case reflect.Map:
		if inner, p, ok := block(t.Elem(), path+".<>"); ok {
			return inner, p, true
		}
	}
	return nil, "", false
}

var (
	codeSpan    = regexp.MustCompile("`([^`\n]+)`")
	placeholder = regexp.MustCompile(`<[^>]*>`)
)

// documentedKeys returns the code spans of a page, with any <placeholder>
// written as <>, so `adapters.<tool>.launch_args` names adapters.<>.launch_args.
func documentedKeys(page string) map[string]bool {
	keys := map[string]bool{}
	for _, match := range codeSpan.FindAllStringSubmatch(page, -1) {
		keys[placeholder.ReplaceAllString(match[1], "<>")] = true
	}
	return keys
}
