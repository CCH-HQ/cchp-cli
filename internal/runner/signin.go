package runner

import (
	"context"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

// SignIn checks the server address and token with whoami, then saves them as
// the current sign-in. Nothing is saved when the server rejects the token.
func (r *Runner) SignIn(ctx context.Context, rawServer, token string) (*auth.Profile, error) {
	server, err := auth.NormalizeServer(rawServer)
	if err != nil {
		return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "%v", err)
	}
	if err := auth.CheckTokenShape(token); err != nil {
		return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "%v", err)
	}
	id, err := r.Whoami(ctx, &auth.Credential{Server: server, Token: token, Source: "login"})
	if err != nil {
		return nil, err
	}
	if id.TokenKind != "" && id.TokenKind != "management-api" {
		return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "this token is a %s credential; cchp signs in with a management token", id.TokenKind)
	}
	profile, err := r.Store.Save(server, token, *id)
	if err != nil {
		return nil, err
	}
	r.UseCredential(&auth.Credential{Server: server, Token: token, Source: r.Store.Backend, Identity: id})
	return profile, nil
}
