package tty

import (
	"bufio"
	"fmt"
	"os"

	"golang.org/x/term"
)

func Prompt(msg string) (string, error) {
	fmt.Fprint(os.Stderr, msg)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return line, nil
}

func PromptPassword(msg string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, fmt.Errorf("password prompt requires a TTY (use --password-stdin for scripted deploys)")
	}
	fmt.Fprint(os.Stderr, msg)
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	return pw, err
}
