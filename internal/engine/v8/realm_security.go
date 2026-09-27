//go:build (windows || linux) && amd64

package v8

import (
	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

func (a *adapter) SetSecurityOrigin(key string) error {
	_, err := a.run(func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		if s.securityTokens == nil {
			s.securityTokens = make(map[string]*gov8.Global)
		}
		token := s.securityTokens[key]
		if token == nil {
			value, err := scope.NewString(key)
			if err != nil {
				return nil, err
			}
			token, err = gov8.NewGlobal(scope, value)
			if err != nil {
				return nil, err
			}
			s.securityTokens[key] = token
		}
		local, err := token.ToLocal(scope)
		if err != nil {
			return nil, err
		}
		return nil, realm.SetSecurityToken(scope, local)
	})
	return err
}
