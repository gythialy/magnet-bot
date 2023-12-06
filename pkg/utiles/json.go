package utiles

import "encoding/json"

func ToString(data interface{}) string {
	b, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return ""
	}
	return string(b)
}
