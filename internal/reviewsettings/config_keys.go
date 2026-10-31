package reviewsettings

import (
	"cmp"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

// ConfigFile names the file that owns a config key, as the README shows it.
type ConfigFile string

// ConfigFile values.
const (
	ConfigFileRepoSettings ConfigFile = ".reviewrules/settings.toml"
	ConfigFileUserConfig   ConfigFile = "~/.config/reviewrules/config.toml"
)

// ConfigKey is one key of a config file: one field of RepoSettings or UserConfig.
type ConfigKey struct {
	Name    string
	File    ConfigFile
	Default string
	Meaning string
	value   func(ReviewConfig) reflect.Value
}

// configFileRow pairs a config file with the struct of ReviewConfig that holds its keys.
type configFileRow struct {
	file   ConfigFile
	values func(ReviewConfig) any
}

func configFileRows() []configFileRow {
	return []configFileRow{
		{file: ConfigFileRepoSettings, values: func(c ReviewConfig) any { return c.Repo }},
		{file: ConfigFileUserConfig, values: func(c ReviewConfig) any { return c.User }},
	}
}

// ConfigKeys lists every config key: the repo settings first, then the user config, each in field order.
func ConfigKeys() []ConfigKey {
	defaults := DefaultReviewConfig()
	var keys []ConfigKey
	for _, row := range configFileRows() {
		structValue := reflect.ValueOf(row.values(defaults))
		for index := range structValue.NumField() {
			field := structValue.Type().Field(index)
			keys = append(keys, ConfigKey{
				Name:    field.Tag.Get("toml"),
				File:    row.file,
				Default: cmp.Or(field.Tag.Get("default-text"), formatConfigValue(structValue.Field(index))),
				Meaning: field.Tag.Get("description"),
				value: func(c ReviewConfig) reflect.Value {
					return reflect.ValueOf(row.values(c)).Field(index)
				},
			})
		}
	}
	return keys
}

// checkUndecodedKeys turns the first key that the decoder did not use into an error. In the user
// config, key holds the case table name first.
func checkUndecodedKeys(file ConfigFile, path string, undecoded []toml.Key) error {
	if len(undecoded) == 0 {
		return nil
	}
	key := undecoded[0]
	name := key
	if file == ConfigFileUserConfig && len(key) > 1 {
		name = key[1:]
	}
	for _, known := range ConfigKeys() {
		if known.Name == name.String() && known.File != file {
			return fmt.Errorf("config key misplaced: %q in %s belongs in %s", known.Name, path, known.File)
		}
	}
	return fmt.Errorf("config key unknown: %q in %s", key.String(), path)
}

// formatConfigValue writes a value as TOML writes it; a struct becomes an inline table.
func formatConfigValue(value reflect.Value) string {
	if value.Kind() == reflect.Struct {
		encoded, err := toml.Marshal(value.Interface())
		if err != nil {
			return fmt.Sprint(value.Interface())
		}
		return "{" + strings.Join(strings.Split(strings.TrimSpace(string(encoded)), "\n"), ", ") + "}"
	}
	if value.Kind() == reflect.Slice && value.Len() == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(value.Interface())
	if err != nil {
		return fmt.Sprint(value.Interface())
	}
	return string(encoded)
}
