package typed

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

func validateInput(value reflect.Value, prefix, location string) error {
	return validateInputValue(value, prefix, location, make(map[uintptr]bool))
}

func validateInputValue(value reflect.Value, prefix, location string, visiting map[uintptr]bool) error {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return nil
		}
		if value.Kind() == reflect.Pointer {
			pointer := value.Pointer()
			if visiting[pointer] {
				return nil
			}
			visiting[pointer] = true
			defer delete(visiting, pointer)
		}
		value = value.Elem()
	}
	value = indirectSafe(value)
	if !value.IsValid() {
		return nil
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if err := validateInputValue(value.Index(i), fmt.Sprintf("%s[%d]", prefix, i), location, visiting); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			name := fmt.Sprintf("%s[%v]", prefix, iterator.Key().Interface())
			if err := validateInputValue(iterator.Value(), name, location, visiting); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
	default:
		return nil
	}
	for i := 0; i < value.NumField(); i++ {
		fieldType := value.Type().Field(i)
		if fieldType.PkgPath != "" || fieldType.Tag.Get("json") == "-" {
			continue
		}
		fieldValue := value.Field(i)
		name := normalizedFieldName(fieldType)
		if prefix != "" && prefix != "body" {
			name = prefix + "." + name
		}
		if violation := validateField(fieldValue, fieldType.Tag.Get("validate")); violation != nil {
			violation.Field = name
			violation.In = location
			return &RequestError{Code: "validation_error", Message: "request validation failed", Violations: []FieldViolation{*violation}}
		}
		if err := validateInputValue(fieldValue, name, location, visiting); err != nil {
			return err
		}
	}
	return nil
}

func validateField(value reflect.Value, tag string) *FieldViolation {
	if tag == "" {
		return nil
	}
	if hasRule(tag, "omitempty") && isZero(value) {
		return nil
	}
	for _, rule := range strings.Split(tag, ",") {
		name, argument, hasArgument := strings.Cut(rule, "=")
		switch name {
		case "", "omitempty":
			continue
		case "required":
			if isZero(value) {
				return violation(name, "field is required")
			}
		case "min", "max", "len":
			if !hasArgument {
				return violation(name, "validation rule is missing a value")
			}
			limit, err := strconv.ParseFloat(argument, 64)
			if err != nil {
				return violation(name, "validation rule has an invalid value")
			}
			actual, ok := measurableValue(value)
			if !ok {
				return violation(name, "validation rule is not supported for this field")
			}
			if name == "min" && actual < limit {
				return violation(name, fmt.Sprintf("must be at least %s", argument))
			}
			if name == "max" && actual > limit {
				return violation(name, fmt.Sprintf("must be at most %s", argument))
			}
			if name == "len" && actual != limit {
				return violation(name, fmt.Sprintf("must have length %s", argument))
			}
		case "oneof":
			actual := fmt.Sprint(interfaceValue(value))
			matched := false
			for _, allowed := range strings.Fields(argument) {
				if actual == allowed {
					matched = true
					break
				}
			}
			if !matched {
				return violation(name, "must be one of: "+argument)
			}
		case "regexp":
			compiled, err := regexp.Compile(argument)
			if err != nil || !compiled.MatchString(fmt.Sprint(interfaceValue(value))) {
				return violation(name, "does not match the required pattern")
			}
		default:
			return violation(name, "unknown validation rule")
		}
	}
	return nil
}

func validateTypeRules(valueType reflect.Type, seen map[reflect.Type]bool) error {
	for valueType.Kind() == reflect.Pointer || valueType.Kind() == reflect.Slice || valueType.Kind() == reflect.Array || valueType.Kind() == reflect.Map {
		valueType = valueType.Elem()
	}
	if valueType.Kind() != reflect.Struct || seen[valueType] {
		return nil
	}
	seen[valueType] = true
	defer delete(seen, valueType)

	for i := 0; i < valueType.NumField(); i++ {
		field := valueType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		validation := field.Tag.Get("validate")
		hasConstraint := false
		for _, rule := range strings.Split(validation, ",") {
			name, argument, hasArgument := strings.Cut(rule, "=")
			switch name {
			case "", "required", "omitempty":
				if hasArgument {
					return fmt.Errorf("field %s validation rule %s does not accept a value", field.Name, name)
				}
			case "min", "max", "len":
				hasConstraint = true
				if !hasArgument {
					return fmt.Errorf("field %s validation rule %s needs a value", field.Name, name)
				}
				number, err := strconv.ParseFloat(argument, 64)
				if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
					return fmt.Errorf("field %s validation rule %s has invalid value %q", field.Name, name, argument)
				}
				if !supportsMeasuredValidation(field.Type) {
					return fmt.Errorf("field %s validation rule %s is not supported for %s", field.Name, name, field.Type)
				}
				if isLengthType(field.Type) && (number < 0 || number != math.Trunc(number)) {
					return fmt.Errorf("field %s validation rule %s needs a non-negative integer for %s", field.Name, name, field.Type)
				}
			case "oneof":
				hasConstraint = true
				if !hasArgument || len(strings.Fields(argument)) == 0 {
					return fmt.Errorf("field %s validation rule oneof needs values", field.Name)
				}
				if !supportsScalarValidation(field.Type) {
					return fmt.Errorf("field %s validation rule oneof is not supported for %s", field.Name, field.Type)
				}
				for _, allowed := range strings.Fields(argument) {
					if err := validateCanonicalScalar(field.Type, allowed); err != nil {
						return fmt.Errorf("field %s validation rule oneof value %q: %w", field.Name, allowed, err)
					}
				}
			case "regexp":
				hasConstraint = true
				if !hasArgument {
					return fmt.Errorf("field %s validation rule regexp needs a value", field.Name)
				}
				if _, err := regexp.Compile(argument); err != nil {
					return fmt.Errorf("field %s validation rule regexp is invalid: %w", field.Name, err)
				}
				if indirectType(field.Type).Kind() != reflect.String {
					return fmt.Errorf("field %s validation rule regexp is not supported for %s", field.Name, field.Type)
				}
			default:
				return fmt.Errorf("field %s has unknown validation rule %s", field.Name, name)
			}
		}
		if hasConstraint && !hasRule(validation, "required") && !hasRule(validation, "omitempty") {
			return fmt.Errorf("field %s validation constraints require required or omitempty", field.Name)
		}
		if err := validateTypeRules(field.Type, seen); err != nil {
			return err
		}
	}
	return nil
}

func isLengthType(valueType reflect.Type) bool {
	switch indirectType(valueType).Kind() {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
		return true
	default:
		return false
	}
}

func validateCanonicalScalar(valueType reflect.Type, value string) error {
	valueType = indirectType(valueType)
	var canonical string
	switch valueType.Kind() {
	case reflect.String:
		return nil
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return errorsNewInvalidScalar()
		}
		canonical = strconv.FormatBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, valueType.Bits())
		if err != nil {
			return errorsNewInvalidScalar()
		}
		canonical = strconv.FormatInt(parsed, 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(value, 10, valueType.Bits())
		if err != nil {
			return errorsNewInvalidScalar()
		}
		canonical = strconv.FormatUint(parsed, 10)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, valueType.Bits())
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return errorsNewInvalidScalar()
		}
		canonical = strconv.FormatFloat(parsed, 'g', -1, valueType.Bits())
	default:
		return errorsNewInvalidScalar()
	}
	if value != canonical {
		return fmt.Errorf("must use canonical value %q", canonical)
	}
	return nil
}

func errorsNewInvalidScalar() error {
	return fmt.Errorf("is not a valid scalar")
}

func indirectType(valueType reflect.Type) reflect.Type {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType
}

func supportsMeasuredValidation(valueType reflect.Type) bool {
	switch indirectType(valueType).Kind() {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func supportsScalarValidation(valueType reflect.Type) bool {
	switch indirectType(valueType).Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func hasRule(tag, wanted string) bool {
	for _, rule := range strings.Split(tag, ",") {
		name, _, _ := strings.Cut(rule, "=")
		if name == wanted {
			return true
		}
	}
	return false
}

func violation(rule, message string) *FieldViolation {
	return &FieldViolation{Rule: rule, Message: message}
}

func indirectSafe(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func isZero(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	return value.IsZero()
}

func measurableValue(value reflect.Value) (float64, bool) {
	value = indirectSafe(value)
	if !value.IsValid() {
		return 0, true
	}
	switch value.Kind() {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
		return float64(value.Len()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(value.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(value.Uint()), true
	case reflect.Float32, reflect.Float64:
		return value.Float(), true
	default:
		return 0, false
	}
}

func interfaceValue(value reflect.Value) any {
	value = indirectSafe(value)
	if !value.IsValid() || !value.CanInterface() {
		return nil
	}
	return value.Interface()
}
