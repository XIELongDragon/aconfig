package aconfig

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func getFields(x interface{}) []*fieldData {
	value := reflect.ValueOf(x)
	for value.Type().Kind() == reflect.Ptr {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		panic("aconfig: only struct can be passed to the loader")
	}
	return getFieldsHelper(value, nil)
}

func getFieldsHelper(valueObject reflect.Value, parent *fieldData) []*fieldData {
	typeObject := valueObject.Type()
	count := valueObject.NumField()

	fields := make([]*fieldData, 0, count)
	for i := 0; i < count; i++ {
		value := valueObject.Field(i)
		field := typeObject.Field(i)

		if !value.CanSet() {
			continue
		}

		fd := newFieldData(field, value, parent)

		// if it's a struct - expand and process it's fields
		if field.Type.Kind() == reflect.Struct {
			var subFieldParent *fieldData
			if field.Anonymous {
				subFieldParent = parent
			} else {
				subFieldParent = fd
			}
			fields = append(fields, getFieldsHelper(value, subFieldParent)...)
			continue
		}
		fields = append(fields, fd)
	}
	return fields
}

type fieldData struct {
	name         string
	parent       *fieldData
	field        reflect.StructField
	value        reflect.Value
	defaultValue string
	usage        string
	jsonName     string
	yamlName     string
	tomlName     string
	envName      string
	flagName     string
}

func newFieldData(field reflect.StructField, value reflect.Value, parent *fieldData) *fieldData {
	words := splitNameByWords(field.Name)
	name := makeFlagName(field, parent, words) // it's ok to use flagName, fields have `_` and nesting is via `.`
	// println(ifNotEmpty(field.Tag.Get(yamlNameTag), "::"+name))

	return &fieldData{
		name:   makeName(field.Name, parent),
		parent: parent,
		value:  value,
		field:  field,

		defaultValue: field.Tag.Get(defaultValueTag),
		usage:        field.Tag.Get(usageTag),
		jsonName:     ifNotEmpty(field.Tag.Get(jsonNameTag), name),
		yamlName:     ifNotEmpty(field.Tag.Get(yamlNameTag), name),
		tomlName:     ifNotEmpty(field.Tag.Get(tomlNameTag), name),
		envName:      ifNotEmpty(field.Tag.Get(envNameTag), makeEnvName(field, parent, words)),
		flagName:     ifNotEmpty(field.Tag.Get(flagNameTag), name),
	}
}

func newSimpleFieldData(value reflect.Value) *fieldData {
	return newFieldData(reflect.StructField{}, value, nil)
}

func (f *fieldData) Name() string {
	return f.name
}

func (f *fieldData) DefaultValue() string {
	return f.defaultValue
}

func (f *fieldData) Usage() string {
	return f.usage
}

func (f *fieldData) fullTag(tag string) string {
	sep := "."
	if tag == envNameTag {
		sep = "_"
	}
	res := f.Tag(tag)
	for p := f.parent; p != nil; p = p.parent {
		res = p.Tag(tag) + sep + res
	}
	return res
}

func (f *fieldData) Tag(tag string) string {
	switch tag {
	case defaultValueTag:
		return f.defaultValue
	case usageTag:
		return f.usage
	case jsonNameTag:
		return f.jsonName
	case yamlNameTag:
		return f.yamlName
	case tomlNameTag:
		return f.tomlName
	case envNameTag:
		return f.envName
	case flagNameTag:
		return f.flagName
	default:
		return f.field.Tag.Get(tag)
	}
}

func (f *fieldData) Parent() (Field, bool) {
	return f.parent, f.parent != nil
}

func setFieldData(field *fieldData, value string) error {
	// unwrap pointers
	for field.value.Type().Kind() == reflect.Ptr {
		if field.value.IsNil() {
			field.value.Set(reflect.New(field.value.Type().Elem()))
		}
		field.value = field.value.Elem()
	}

	if value == "" {
		return nil
	}

	switch kind := field.value.Type().Kind(); kind {
	case reflect.Bool:
		return setBool(field, value)

	case reflect.String:
		return setString(field, value)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return setInt(field, value)

	case reflect.Int64:
		return setInt64(field, value)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return setUint(field, value)

	case reflect.Float32, reflect.Float64:
		return setFloat(field, value)

	case reflect.Slice:
		return setSlice(field, value)

	case reflect.Map:
		return setMap(field, value)

	default:
		return fmt.Errorf("type kind %q isn't supported", kind)
	}
}

func setBool(field *fieldData, value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	field.value.SetBool(val)
	return nil
}

func setInt(field *fieldData, value string) error {
	val, err := strconv.ParseInt(value, 0, field.value.Type().Bits())
	if err != nil {
		return err
	}
	field.value.SetInt(val)
	return nil
}

func setInt64(field *fieldData, value string) error {
	if field.field.Type == reflect.TypeOf(time.Second) {
		val, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		field.value.Set(reflect.ValueOf(val))
		return nil
	}
	return setInt(field, value)
}

func setUint(field *fieldData, value string) error {
	val, err := strconv.ParseUint(value, 0, field.value.Type().Bits())
	if err != nil {
		return err
	}
	field.value.SetUint(val)
	return nil
}

func setFloat(field *fieldData, value string) error {
	val, err := strconv.ParseFloat(value, field.value.Type().Bits())
	if err != nil {
		return err
	}
	field.value.SetFloat(val)
	return nil
}

func setString(field *fieldData, value string) error {
	field.value.SetString(value)
	return nil
}

func setSlice(field *fieldData, value string) error {
	// Special case for []byte
	if field.field.Type.Elem().Kind() == reflect.Uint8 {
		value := reflect.ValueOf([]byte(value))
		field.value.Set(value)
		return nil
	}

	vals := strings.Split(value, ",")
	slice := reflect.MakeSlice(field.field.Type, len(vals), len(vals))
	for i, val := range vals {
		val = strings.TrimSpace(val)

		fd := newFieldData(reflect.StructField{}, slice.Index(i), nil)
		if err := setFieldData(fd, val); err != nil {
			return fmt.Errorf("incorrect slice item %q: %w", val, err)
		}
	}
	field.value.Set(slice)
	return nil
}

func setMap(field *fieldData, value string) error {
	vals := strings.Split(value, ",")
	mapField := reflect.MakeMapWithSize(field.field.Type, len(vals))

	for _, val := range vals {
		entry := strings.SplitN(val, ":", 2)
		if len(entry) != 2 {
			return fmt.Errorf("incorrect map item: %s", val)
		}
		key := strings.TrimSpace(entry[0])
		val := strings.TrimSpace(entry[1])

		fdk := newSimpleFieldData(reflect.New(field.field.Type.Key()).Elem())
		if err := setFieldData(fdk, key); err != nil {
			return fmt.Errorf("incorrect map key %q: %w", key, err)
		}

		fdv := newSimpleFieldData(reflect.New(field.field.Type.Elem()).Elem())
		if err := setFieldData(fdv, val); err != nil {
			return fmt.Errorf("incorrect map value %q: %w", val, err)
		}
		mapField.SetMapIndex(fdk.value, fdv.value)
	}
	field.value.Set(mapField)
	return nil
}
