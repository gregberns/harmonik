package evalvol

import (
	"errors"
	"fmt"
	"strconv"
)

var (
	ErrDivideByZero = errors.New("divide by zero")
	ErrMalformed    = errors.New("malformed expression")
)

// EvalRPN evaluates a Reverse Polish Notation expression.
// Supported operators: + - * /
func EvalRPN(tokens []string) (float64, error) {
	stack := make([]float64, 0, len(tokens))

	for _, tok := range tokens {
		switch tok {
		case "+", "-", "*", "/":
			if len(stack) < 2 {
				return 0, fmt.Errorf("%w: operator %q requires two operands", ErrMalformed, tok)
			}
			b := stack[len(stack)-1]
			a := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			var result float64
			switch tok {
			case "+":
				result = a + b
			case "-":
				result = a - b
			case "*":
				result = a * b
			case "/":
				if b == 0 {
					return 0, ErrDivideByZero
				}
				result = a / b
			}
			stack = append(stack, result)
		default:
			v, err := strconv.ParseFloat(tok, 64)
			if err != nil {
				return 0, fmt.Errorf("%w: invalid token %q", ErrMalformed, tok)
			}
			stack = append(stack, v)
		}
	}

	if len(stack) != 1 {
		return 0, fmt.Errorf("%w: expression left %d values on stack", ErrMalformed, len(stack))
	}
	return stack[0], nil
}
