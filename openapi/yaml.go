package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func marshalYAML(document []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}

	var result bytes.Buffer
	if err := writeYAMLValue(&result, value, 0); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

func writeYAMLValue(output *bytes.Buffer, value any, indentation int) error {
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			output.WriteString(strings.Repeat(" ", indentation))
			output.WriteString("{}\n")
			return nil
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			output.WriteString(strings.Repeat(" ", indentation))
			output.WriteString(quoteYAMLString(key))
			output.WriteByte(':')
			if isYAMLScalar(typed[key]) {
				output.WriteByte(' ')
				if err := writeYAMLScalar(output, typed[key]); err != nil {
					return err
				}
				output.WriteByte('\n')
				continue
			}
			if collectionLength(typed[key]) == 0 {
				output.WriteByte(' ')
				if _, ok := typed[key].([]any); ok {
					output.WriteString("[]\n")
				} else {
					output.WriteString("{}\n")
				}
				continue
			}
			output.WriteByte('\n')
			if err := writeYAMLValue(output, typed[key], indentation+2); err != nil {
				return err
			}
		}
		return nil

	case []any:
		if len(typed) == 0 {
			output.WriteString(strings.Repeat(" ", indentation))
			output.WriteString("[]\n")
			return nil
		}
		for _, item := range typed {
			output.WriteString(strings.Repeat(" ", indentation))
			output.WriteByte('-')
			if isYAMLScalar(item) {
				output.WriteByte(' ')
				if err := writeYAMLScalar(output, item); err != nil {
					return err
				}
				output.WriteByte('\n')
				continue
			}
			if collectionLength(item) == 0 {
				output.WriteByte(' ')
				if _, ok := item.([]any); ok {
					output.WriteString("[]\n")
				} else {
					output.WriteString("{}\n")
				}
				continue
			}
			output.WriteByte('\n')
			if err := writeYAMLValue(output, item, indentation+2); err != nil {
				return err
			}
		}
		return nil

	default:
		output.WriteString(strings.Repeat(" ", indentation))
		if err := writeYAMLScalar(output, value); err != nil {
			return err
		}
		output.WriteByte('\n')
		return nil
	}
}

func writeYAMLScalar(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case string:
		output.WriteString(quoteYAMLString(typed))
	case bool:
		output.WriteString(strconv.FormatBool(typed))
	case json.Number:
		output.WriteString(typed.String())
	default:
		return fmt.Errorf("unsupported YAML scalar %T", value)
	}
	return nil
}

func quoteYAMLString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func isYAMLScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, json.Number:
		return true
	default:
		return false
	}
}

func collectionLength(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		return len(typed)
	case []any:
		return len(typed)
	default:
		return -1
	}
}
