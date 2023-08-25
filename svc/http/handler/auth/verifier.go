package auth

func EqString(eqValue string) Verifier {
	return func(value any) (error, bool) {
		strValue, ok := value.(string)
		if !ok {
			return ErrWrongType, false
		}
		return nil, strValue == eqValue
	}
}

func EmptyString() Verifier {
	return func(value any) (error, bool) {
		strValue, ok := value.(string)
		if !ok {
			return ErrWrongType, false
		}
		return nil, strValue == ""
	}
}

func Not(v Verifier) Verifier {
	return func(value any) (error, bool) {
		err, result := v(value)
		if err != nil {
			return err, false
		}
		return nil, !result
	}
}
