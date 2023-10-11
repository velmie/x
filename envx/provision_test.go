package envx_test

import (
	"errors"
	"testing"

	. "github.com/velmie/x/envx"
)

func Test_Set(t *testing.T) {
	var target int

	getterWithNoError := func() (int, error) {
		return 42, nil
	}

	getterWithError := func() (int, error) {
		return 0, errors.New("getter error")
	}

	setter := Set(&target, getterWithNoError)
	err := setter()
	if err != nil {
		t.Errorf("expected no error, but got %v", err)
	}
	if target != 42 {
		t.Errorf("expected target to be set to 42, but got %d", target)
	}

	setter = Set(&target, getterWithError)
	err = setter()
	if err == nil || err.Error() != "getter error" {
		t.Errorf("expected 'getter error', but got %v", err)
	}
}

func Test_Supply(t *testing.T) {
	setterNoError := func() error {
		return nil
	}

	setterWithError := func() error {
		return errors.New("setter error")
	}

	err := Supply(setterNoError, setterWithError, setterNoError)
	if err == nil || err.Error() != "setter error" {
		t.Errorf("expected 'setter error', but got %v", err)
	}

	err = Supply(setterNoError, setterNoError)
	if err != nil {
		t.Errorf("expected no error, but got %v", err)
	}
}
