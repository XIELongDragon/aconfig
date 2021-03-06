package aconfig

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultValueTag = "default"
	usageTag        = "usage"
	jsonNameTag     = "json"
	yamlNameTag     = "yaml"
	tomlNameTag     = "toml"
	envNameTag      = "env"
	flagNameTag     = "flag"
)

// Loader of user configuration.
type Loader struct {
	config  Config
	dst     interface{}
	fields  []*fieldData
	flagSet *flag.FlagSet
}

// Config to configure configuration loader.
type Config struct {
	SkipDefaults    bool
	SkipFiles       bool
	SkipEnvironment bool
	SkipFlags       bool

	EnvPrefix  string
	FlagPrefix string

	StopOnFileError bool
	Files           []string
	FileDecoders    map[string]FileDecoder
}

// FileDecoder is used to read config from files. See aconfig submodules.
type FileDecoder interface {
	DecodeFile(filename string) (map[string]interface{}, error)
}

// Field of the user configuration structure.
// Done as an interface to export less things in lib.
type Field interface {
	// Name of the field.
	Name() string

	// DefaultValue of the field.
	DefaultValue() string

	// Usage of the field (set in `usage` tag).
	Usage() string

	// Tag returns a given tag for a field.
	Tag(tag string) string

	// Parent of the current node.
	Parent() (Field, bool)
}

// LoaderFor creates a new Loader based on a given configuration structure.
func LoaderFor(dst interface{}, cfg Config) *Loader {
	l := &Loader{
		dst:    dst,
		config: cfg,
	}
	l.init()
	return l
}

func (l *Loader) init() {
	if l.config.EnvPrefix != "" {
		l.config.EnvPrefix += "_"
	}

	if l.config.FlagPrefix != "" {
		l.config.FlagPrefix += "."
	}

	if _, ok := l.config.FileDecoders[".json"]; !ok {
		if l.config.FileDecoders == nil {
			l.config.FileDecoders = map[string]FileDecoder{}
		}
		l.config.FileDecoders[".json"] = &jsonDecoder{}
	}

	l.flagSet = flag.NewFlagSet(l.config.FlagPrefix, flag.ContinueOnError)
	l.parseFields()
}

func (l *Loader) parseFields() {
	l.fields = getFields(l.dst)

	if !l.config.SkipFlags {
		for _, field := range l.fields {
			flagName := l.config.FlagPrefix + field.fullTag(flagNameTag)
			l.flagSet.String(flagName, field.defaultValue, field.usage)
		}
	}
}

// Flags returngs flag.FlagSet to create your own flags.
func (l *Loader) Flags() *flag.FlagSet {
	return l.flagSet
}

// WalkFields iterates over configuration fields.
// Easy way to create documentation or other stuff.
func (l *Loader) WalkFields(fn func(f Field) bool) {
	for _, f := range l.fields {
		if !fn(f) {
			return
		}
	}
}

// Load configuration into a given param.
func (l *Loader) Load() error {
	if err := l.loadSources(); err != nil {
		return fmt.Errorf("aconfig: cannot load config: %w", err)
	}
	return nil
}

// LoadWithFile configuration into a given param.
func (l *Loader) LoadWithFile(file string) error {
	l.config.Files = []string{file}
	return l.Load()
}

func (l *Loader) loadSources() error {
	if !l.config.SkipDefaults {
		if err := l.loadDefaults(); err != nil {
			return err
		}
	}
	if !l.config.SkipFiles {
		if err := l.loadFromFile(); err != nil {
			return err
		}
	}
	if !l.config.SkipEnvironment {
		if err := l.loadEnvironment(); err != nil {
			return err
		}
	}
	if !l.config.SkipFlags {
		if err := l.loadFlags(); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loader) loadDefaults() error {
	for _, fd := range l.fields {
		if err := setFieldData(fd, fd.defaultValue); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loader) loadFromFile() error {
	for _, file := range l.config.Files {
		ext := strings.ToLower(filepath.Ext(file))
		decoder, ok := l.config.FileDecoders[ext]
		if !ok {
			return fmt.Errorf("file format '%q' isn't supported", ext)
		}

		mappedFields, err := decoder.DecodeFile(file)
		if err != nil {
			return err
		}

		tag := ext[1:]

		for _, field := range l.fields {
			name := field.fullTag(tag)
			// fmt.Printf("search %#v\n", name)
			value, ok := mappedFields[name]
			if !ok {
				continue
			}

			if value, ok := value.(string); ok {
				if err := setFieldData(field, value); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return nil
}

func (l *Loader) loadEnvironment() error {
	for _, field := range l.fields {
		envName := l.config.EnvPrefix + field.fullTag(envNameTag)
		// println(envName)
		v, ok := os.LookupEnv(envName)
		if !ok {
			continue
		}
		if err := setFieldData(field, v); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loader) loadFlags() error {
	if !l.flagSet.Parsed() {
		if err := l.flagSet.Parse(os.Args[1:]); err != nil {
			return err
		}
	}

	actualFlags := map[string]*flag.Flag{}
	l.flagSet.Visit(func(f *flag.Flag) {
		actualFlags[f.Name] = f
	})

	for _, field := range l.fields {
		flagName := l.config.FlagPrefix + field.fullTag(flagNameTag)
		flg, ok := actualFlags[flagName]
		if !ok {
			continue
		}
		if err := setFieldData(field, flg.Value.String()); err != nil {
			return err
		}
	}
	return nil
}
