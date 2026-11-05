package reviewsettings

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/popoffvg/reviewrules/internal/ruleset"
)

// defaultSource is the source of a value that no file sets.
const defaultSource = "default"

// ReviewConfig is the merged config of one run and the file that gave each value.
type ReviewConfig struct {
	Repo RepoSettings
	User UserConfig
	// sources maps a printed key name to the file that set it; a missing name is a default.
	sources map[string]string
}

// DefaultReviewConfig is what a run uses with no config file.
func DefaultReviewConfig() ReviewConfig {
	return ReviewConfig{Repo: DefaultRepoSettings(), User: DefaultUserConfig(), sources: map[string]string{}}
}

// LoadReviewConfig reads the global settings, the repo settings of repoRoot, and one case of the
// user config over the defaults. A missing file or key keeps the value below it.
func LoadReviewConfig(userConfigPath string, userCase UserConfigCase, repoRoot string) (ReviewConfig, error) {
	config := DefaultReviewConfig()

	for _, path := range []string{
		globalSettingsPath(userConfigPath),
		filepath.Join(repoRoot, ".reviewrules", "settings.toml"),
	} {
		err := config.decodeRepoSettings(path)
		if err != nil {
			return config, err
		}
	}

	file := userConfigFile{Local: DefaultUserConfig(), CI: DefaultUserConfig()}
	meta, err := toml.DecodeFile(userConfigPath, &file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return config, fmt.Errorf("user config parse failed for %s: %w", userConfigPath, err)
	default:
		err = checkUndecodedKeys(ConfigFileUserConfig, userConfigPath, meta.Undecoded())
		if err != nil {
			return config, err
		}
		config.User = file.Local
		if userCase == UserConfigCaseCI {
			config.User = file.CI
		}
		config.recordSources(ConfigFileUserConfig, meta, toml.Key{string(userCase)}, fmt.Sprintf("%s [%s]", userConfigPath, userCase))
	}
	return config, config.User.validate()
}

// globalSettingsPath is settings.toml in the folder of the user config. It holds repo settings
// keys for every repo; a key in a repo's .reviewrules/settings.toml replaces its value.
func globalSettingsPath(userConfigPath string) string {
	return filepath.Join(filepath.Dir(userConfigPath), "settings.toml")
}

// decodeRepoSettings reads one settings file over c.Repo. A key the file sets replaces the whole
// value, a list too; a missing file changes nothing.
func (c *ReviewConfig) decodeRepoSettings(path string) error {
	meta, err := toml.DecodeFile(path, &c.Repo)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("repo settings parse failed for %s: %w", path, err)
	}
	err = checkUndecodedKeys(ConfigFileRepoSettings, path, meta.Undecoded())
	if err != nil {
		return err
	}
	c.recordSources(ConfigFileRepoSettings, meta, nil, path)
	return c.Repo.validate(path)
}

// recordSources marks every key of file that the decoded file defines under prefix. A map key
// records each of its entries; a list of tables records each entry when the file defines the list.
func (c *ReviewConfig) recordSources(file ConfigFile, meta toml.MetaData, prefix toml.Key, source string) {
	for _, key := range ConfigKeys() {
		if key.File != file {
			continue
		}
		isTableList := key.value(*c).Kind() == reflect.Slice
		for _, name := range c.printedNames(key) {
			defined := name
			if isTableList {
				defined = name[:1]
			}
			if meta.IsDefined(slices.Concat(prefix, defined)...) {
				c.sources[name.String()] = source
			}
		}
	}
}

// printedNames is the key name, or one name per entry of a map key in sorted order, or one name
// per entry of a non-empty list of tables in list order: the entry's Name field, else its
// 1-based position, since the position decides which profile matches first.
func (c ReviewConfig) printedNames(key ConfigKey) []toml.Key {
	value := key.value(c)
	switch {
	case value.Kind() == reflect.Map:
		var names []toml.Key
		for _, entry := range value.MapKeys() {
			names = append(names, toml.Key{key.Name, entry.String()})
		}
		slices.SortFunc(names, func(a, b toml.Key) int { return cmp.Compare(a[1], b[1]) })
		return names
	case value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Struct && value.Len() > 0:
		names := make([]toml.Key, 0, value.Len())
		for index := range value.Len() {
			name := strconv.Itoa(index + 1)
			if field := value.Index(index).FieldByName("Name"); field.IsValid() {
				name = field.String()
			}
			names = append(names, toml.Key{key.Name, name})
		}
		return names
	}
	return []toml.Key{{key.Name}}
}

// ruleSetNotFetched is the commit text of a rule set that the cache does not hold.
const ruleSetNotFetched = "not fetched"

// WriteMergedConfig writes one line per value: the key, the value, and the file that gave it or "default".
// A rule set line adds its commit from ruleSetCommits, keyed by rule set name.
func (c ReviewConfig) WriteMergedConfig(out io.Writer, ruleSetCommits map[string]string) error {
	for _, key := range ConfigKeys() {
		value := key.value(c)
		for index, name := range c.printedNames(key) {
			entry := value
			switch {
			case value.Kind() == reflect.Map:
				entry = value.MapIndex(reflect.ValueOf(name[1]))
			case len(name) > 1:
				entry = value.Index(index)
			}
			text := formatConfigValue(entry)
			if set, isRuleSet := entry.Interface().(ruleset.RuleSet); isRuleSet {
				text = strings.TrimSuffix(text, "}") + fmt.Sprintf(", commit = %q}", cmp.Or(ruleSetCommits[set.Name], ruleSetNotFetched))
			}
			source, found := c.sources[name.String()]
			if !found {
				source = defaultSource
			}
			_, err := fmt.Fprintf(out, "%-32s = %-48s # %s\n", name.String(), text, source)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
