package game

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
)

// Recipes are trusted, versioned local configuration. Captured task text is data,
// passed as JSON via base64 rather than interpolated into Python or shell code.
type Recipe struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Python  string `json:"python"`
}

func recipeCommand(text string, recipes []Recipe) (name, command string) {
	for _, recipe := range recipes {
		re, err := regexp.Compile(recipe.Pattern)
		if err != nil {
			continue
		}
		match := re.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		args := map[string]string{"task": text}
		for i, key := range re.SubexpNames() {
			if i > 0 && key != "" {
				args[key] = match[i]
			}
		}
		raw, _ := json.Marshal(args)
		data := base64.StdEncoding.EncodeToString(raw)
		script := base64.StdEncoding.EncodeToString([]byte(recipe.Python))
		return recipe.Name, "python3 -c \"import base64,sys;sys.argv=['recipe',base64.b64decode('" + data + "').decode('utf-8')];exec(compile(base64.b64decode('" + script + "'),'<recipe>','exec'))\""
	}
	return "", ""
}
func recipeAnswer(result string) (string, bool) {
	if !strings.HasPrefix(result, "[exitCode:0]\n") || strings.Contains(result, "[TRUNCATED]") {
		return "", false
	}
	var value struct {
		Answer *string `json:"answer"`
	}
	err := json.Unmarshal([]byte(strings.TrimPrefix(result, "[exitCode:0]\n")), &value)
	if err != nil || value.Answer == nil {
		return "", false
	}
	return *value.Answer, true
}
