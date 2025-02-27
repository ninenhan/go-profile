package utils

func Ternary[T any](cond bool, trueVal T, falseVal T) T {
	if cond {
		return trueVal
	}
	return falseVal
}
