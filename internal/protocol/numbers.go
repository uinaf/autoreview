package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type wireInt int

func (value *wireInt) UnmarshalJSON(data []byte) error {
	parsed, err := exactInt64(data)
	if err != nil {
		return err
	}
	converted := int(parsed)
	if int64(converted) != parsed {
		return fmt.Errorf("integer %s is out of range", data)
	}
	*value = wireInt(converted)
	return nil
}

type wireInt64 int64

func (value *wireInt64) UnmarshalJSON(data []byte) error {
	parsed, err := exactInt64(data)
	if err != nil {
		return err
	}
	*value = wireInt64(parsed)
	return nil
}

func exactInt64(data []byte) (int64, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || (data[0] != '-' && (data[0] < '0' || data[0] > '9')) {
		return 0, fmt.Errorf("must be a JSON number")
	}
	var syntax json.Number
	if err := json.Unmarshal(data, &syntax); err != nil {
		return 0, fmt.Errorf("must be a JSON number: %w", err)
	}
	literal := syntax.String()
	negative := strings.HasPrefix(literal, "-")
	if negative {
		literal = literal[1:]
	}

	mantissa := literal
	exponentText := ""
	if index := strings.IndexAny(literal, "eE"); index >= 0 {
		mantissa = literal[:index]
		exponentText = literal[index+1:]
	}
	fractionDigits := 0
	if index := strings.IndexByte(mantissa, '.'); index >= 0 {
		fractionDigits = len(mantissa) - index - 1
		mantissa = mantissa[:index] + mantissa[index+1:]
	}
	if strings.Trim(mantissa, "0") == "" {
		return 0, nil
	}

	exponent := boundedExponent(exponentText, len(mantissa)+fractionDigits+20)
	scale := exponent - fractionDigits
	if scale < 0 {
		trailingZeros := len(mantissa) - len(strings.TrimRight(mantissa, "0"))
		if -scale > trailingZeros {
			return 0, fmt.Errorf("must be an exactly representable integer")
		}
		mantissa = mantissa[:len(mantissa)+scale]
	} else if scale > 0 {
		significant := strings.TrimLeft(mantissa, "0")
		if len(significant)+scale > 19 {
			return 0, fmt.Errorf("must be an exactly representable signed 64-bit integer")
		}
		mantissa = significant + strings.Repeat("0", scale)
	}

	mantissa = strings.TrimLeft(mantissa, "0")
	if mantissa == "" {
		return 0, nil
	}
	if negative {
		mantissa = "-" + mantissa
	}
	value, err := strconv.ParseInt(mantissa, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("must be an exactly representable integer")
	}
	return value, nil
}

func boundedExponent(value string, limit int) int {
	if value == "" {
		return 0
	}
	negative := value[0] == '-'
	if negative || value[0] == '+' {
		value = value[1:]
	}
	exponent := 0
	for _, digit := range value {
		amount := int(digit - '0')
		if exponent > (limit-amount)/10 {
			exponent = limit + 1
			break
		}
		exponent = exponent*10 + amount
	}
	if negative {
		return -exponent
	}
	return exponent
}

func (location *Location) UnmarshalJSON(data []byte) error {
	var wire struct {
		FilePath  string  `json:"file_path"`
		StartLine wireInt `json:"start_line"`
		EndLine   wireInt `json:"end_line"`
	}
	if err := decodeOne(data, &wire); err != nil {
		return err
	}
	*location = Location{
		FilePath:  wire.FilePath,
		StartLine: int(wire.StartLine),
		EndLine:   int(wire.EndLine),
	}
	return nil
}

func (lineRange *LineRange) UnmarshalJSON(data []byte) error {
	var wire struct {
		StartLine wireInt `json:"start_line"`
		EndLine   wireInt `json:"end_line"`
	}
	if err := decodeOne(data, &wire); err != nil {
		return err
	}
	*lineRange = LineRange{StartLine: int(wire.StartLine), EndLine: int(wire.EndLine)}
	return nil
}

func (attempt *Attempt) UnmarshalJSON(data []byte) error {
	var wire struct {
		Number     wireInt        `json:"number"`
		Outcome    AttemptOutcome `json:"outcome"`
		DurationMS wireInt64      `json:"duration_ms"`
		ErrorClass *FailureClass  `json:"error_class"`
	}
	if err := decodeOne(data, &wire); err != nil {
		return err
	}
	*attempt = Attempt{
		Number:     int(wire.Number),
		Outcome:    wire.Outcome,
		DurationMS: int64(wire.DurationMS),
		ErrorClass: wire.ErrorClass,
	}
	return nil
}

func (metadata *Metadata) UnmarshalJSON(data []byte) error {
	var wire struct {
		Target           *Target          `json:"target"`
		Provider         *Provider        `json:"provider"`
		Attempts         []Attempt        `json:"attempts"`
		DurationMS       wireInt64        `json:"duration_ms"`
		Isolation        *Isolation       `json:"isolation"`
		WebAccess        bool             `json:"web_access"`
		ProtocolRecovery ProtocolRecovery `json:"protocol_recovery"`
	}
	if err := decodeOne(data, &wire); err != nil {
		return err
	}
	*metadata = Metadata{
		Target:           wire.Target,
		Provider:         wire.Provider,
		Attempts:         wire.Attempts,
		DurationMS:       int64(wire.DurationMS),
		Isolation:        wire.Isolation,
		WebAccess:        wire.WebAccess,
		ProtocolRecovery: wire.ProtocolRecovery,
	}
	return nil
}
