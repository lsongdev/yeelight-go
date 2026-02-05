package cli

import (
	"os"
	"regexp"
	"strings"
)

func ParseArgs() ([]string, map[string]any) {
	args := []string{}
	flags := map[string]any{}
	re := regexp.MustCompile(`^--(\w+)(=(.+))?$`)
	for _, arg := range os.Args[1:] {
		if re.Match([]byte(arg)) {
			p := re.FindStringSubmatch(arg)
			k := p[1]
			var v any
			v = p[3]
			if v == "" {
				v = true
			}
			flags[k] = v
		} else if strings.HasPrefix(arg, "-") {
			for _, v := range arg[1:] {
				flags[string(v)] = true
			}
		} else {
			args = append(args, arg)
		}
	}
	return args, flags
}