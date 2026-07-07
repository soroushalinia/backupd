package config

import (
	"github.com/mitchellh/mapstructure"
)

func decode(input map[string]any, output *Config) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           output,
		WeaklyTypedInput: true,
		TagName:          "mapstructure",
	})
	if err != nil {
		return err
	}
	return decoder.Decode(input)
}
