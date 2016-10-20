package main

import fp "path/filepath"
import t "html/template"
import pc "github.com/maklesoft/padlock-cloud/padlockcloud"

// Wrapper for holding references to template instances used for rendering emails, webpages etc.
type Templates struct {
	*pc.Templates
	ActivateAuthTokenSuccess *t.Template
	Dashboard                *t.Template
}

// Loads templates from given directory
func LoadTemplates(tt *Templates, p string) error {
	var err error

	if tt.ActivateAuthTokenSuccess, err = pc.ExtendTemplate(
		tt.BasePage,
		fp.Join(p, "page/activate-auth-token-success.html"),
	); err != nil {
		return err
	}

	if tt.Dashboard, err = pc.ExtendTemplate(
		tt.Templates.Dashboard,
		fp.Join(p, "page/dashboard.html"),
	); err != nil {
		return err
	}

	return nil
}
