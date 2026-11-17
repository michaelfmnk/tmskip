package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ask prints a yes/no question and reads the answer from stdin. An empty
// answer is def; end of input is "no".
func (a *app) ask(q string, def bool) (bool, error) {
	if a.stdin == nil {
		a.stdin = bufio.NewReader(orEmpty(a.d.Env.Stdin))
	}
	choices := " [y/N] "
	if def {
		choices = " [Y/n] "
	}
	for {
		fmt.Fprint(a.d.Env.Stdout, q+choices)
		line, err := a.stdin.ReadString('\n')
		if err != nil && err != io.EOF {
			return false, err
		}
		ans := strings.ToLower(strings.TrimSpace(line))
		switch {
		case ans == "" && err == io.EOF:
			fmt.Fprintln(a.d.Env.Stdout)
			return false, nil
		case ans == "":
			return def, nil
		case ans == "y" || ans == "yes":
			return true, nil
		case ans == "n" || ans == "no":
			return false, nil
		}
		if err == io.EOF {
			return false, nil
		}
	}
}
