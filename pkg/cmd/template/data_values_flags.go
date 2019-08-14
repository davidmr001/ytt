package template

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/k14s/ytt/pkg/yamlmeta"
	"github.com/spf13/cobra"
)

type DataValuesFlags struct {
	Env          []string
	EnvAsStrings []string

	KVs          []string
	KVsAsStrings []string

	Files []string

	Inspect bool
}

func (s *DataValuesFlags) Set(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&s.Env, "data-values-env", nil, "Extract _yaml_ data values from environment with given prefix (format: PREFIX for vars like PREFIX_all__key1=\"str\") (can be specified multiple times)")
	cmd.Flags().StringArrayVar(&s.EnvAsStrings, "data-values-env-as-strings", nil, "Extract _string_ data values from environment with given prefix (format: PREFIX for vars like PREFIX_all__key1=\"str\") (can be specified multiple times)")
	cmd.Flags().StringArrayVarP(&s.KVs, "data-value", "v", nil, "Set specific data value to given _yaml_ value (format: all.key1.subkey=123, all.key2=\"str\") (can be specified multiple times)")
	cmd.Flags().StringArrayVar(&s.KVsAsStrings, "data-value-as-string", nil, "Set specific data value to given _string_ value (format: all.key1.subkey=123) (can be specified multiple times)")
	cmd.Flags().StringArrayVar(&s.Files, "data-value-file", nil, "Set specific data value to given file contents as string (format: all.key1.subkey=/file/path) (can be specified multiple times)")
	cmd.Flags().BoolVar(&s.Inspect, "data-values-inspect", false, "Inspect data values")
}

type dataValuesFlagsSource struct {
	Values        []string
	TransformFunc func(string) (interface{}, error)
}

func (s *DataValuesFlags) Values(strict bool) (map[interface{}]interface{}, error) {
	plainValFunc := func(rawVal string) (interface{}, error) { return rawVal, nil }

	yamlValFunc := func(rawVal string) (interface{}, error) {
		val, err := s.parseYAML(rawVal, strict)
		if err != nil {
			return nil, fmt.Errorf("Deserializing YAML value: %s", err)
		}
		return val, nil
	}

	result := []map[string]interface{}{}

	for _, src := range []dataValuesFlagsSource{{s.Env, yamlValFunc}, {s.EnvAsStrings, plainValFunc}} {
		for _, envPrefix := range src.Values {
			vals, err := s.env(envPrefix, src.TransformFunc)
			if err != nil {
				return nil, fmt.Errorf("Extracting data values from env under prefix '%s': %s", envPrefix, err)
			}
			result = append(result, vals)
		}
	}

	// KVs and files take precedence over environment variables
	for _, src := range []dataValuesFlagsSource{{s.KVs, yamlValFunc}, {s.KVsAsStrings, plainValFunc}} {
		for _, kv := range src.Values {
			vals, err := s.kv(kv, src.TransformFunc)
			if err != nil {
				return nil, fmt.Errorf("Extracting data value from KV: %s", err)
			}
			result = append(result, vals)
		}
	}

	for _, file := range s.Files {
		vals, err := s.file(file)
		if err != nil {
			return nil, fmt.Errorf("Extracting data value from file: %s", err)
		}
		result = append(result, vals)
	}

	return s.convertIntoNestedMap(result)
}

func (s *DataValuesFlags) env(prefix string, valueFunc func(string) (interface{}, error)) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	envVars := os.Environ()

	for _, envVar := range envVars {
		pieces := strings.SplitN(envVar, "=", 2)
		if len(pieces) != 2 {
			return nil, fmt.Errorf("Expected env variable to be key-value pair (format: key=value)")
		}

		if !strings.HasPrefix(pieces[0], prefix+"_") {
			continue
		}

		val, err := valueFunc(pieces[1])
		if err != nil {
			return nil, fmt.Errorf("Extracting data value from env variable '%s': %s", pieces[0], err)
		}

		// '__' gets translated into a '.' since periods may not be liked by shells
		result[strings.Replace(strings.TrimPrefix(pieces[0], prefix+"_"), "__", ".", -1)] = val
	}

	return result, nil
}

func (s *DataValuesFlags) kv(kv string, valueFunc func(string) (interface{}, error)) (map[string]interface{}, error) {
	result := map[string]interface{}{}

	pieces := strings.SplitN(kv, "=", 2)
	if len(pieces) != 2 {
		return nil, fmt.Errorf("Expected format key=value")
	}

	val, err := valueFunc(pieces[1])
	if err != nil {
		return nil, fmt.Errorf("Deserializing value for key '%s': %s", pieces[0], err)
	}

	result[pieces[0]] = val

	return result, nil
}

func (s *DataValuesFlags) parseYAML(data string, strict bool) (interface{}, error) {
	docSet, err := yamlmeta.NewParser(yamlmeta.ParserOpts{Strict: strict}).ParseBytes([]byte(data), "")
	if err != nil {
		return nil, err
	}
	return docSet.Items[0].Value, nil
}

func (s *DataValuesFlags) file(kv string) (map[string]interface{}, error) {
	result := map[string]interface{}{}

	pieces := strings.SplitN(kv, "=", 2)
	if len(pieces) != 2 {
		return nil, fmt.Errorf("Expected format key=/file/path")
	}

	contents, err := ioutil.ReadFile(pieces[1])
	if err != nil {
		return nil, fmt.Errorf("Reading file '%s'", pieces[1])
	}

	result[pieces[0]] = string(contents)

	return result, nil
}

func (s *DataValuesFlags) convertIntoNestedMap(multipleVals []map[string]interface{}) (map[interface{}]interface{}, error) {
	result := map[interface{}]interface{}{}
	for _, vals := range multipleVals {
		for key, val := range vals {
			keyPieces := strings.Split(key, ".")
			currMap := result
			for _, keyPiece := range keyPieces[:len(keyPieces)-1] {
				if subMap, found := currMap[keyPiece]; found {
					if typedSubMap, ok := subMap.(map[interface{}]interface{}); ok {
						currMap = typedSubMap
					} else {
						return nil, fmt.Errorf("Expected key '%s' to not conflict with other data values at piece '%s'", key, keyPiece)
					}
				} else {
					newCurrMap := map[interface{}]interface{}{}
					currMap[keyPiece] = newCurrMap
					currMap = newCurrMap
				}
			}
			currMap[keyPieces[len(keyPieces)-1]] = val
		}
	}
	return result, nil
}
