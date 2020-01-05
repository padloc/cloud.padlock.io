package main

import (
	"fmt"
	pc "github.com/padloc/padlock-cloud/padlockcloud"
	"github.com/stripe/stripe-go"
	t "html/template"
	fp "path/filepath"
	"strings"
	"time"
)

// Wrapper for holding references to template instances used for rendering emails, webpages etc.
type Templates struct {
	*pc.Templates
	// Dashboard *t.Template
	Invoice     *t.Template
	InvoiceList *t.Template
}

// Loads templates from given directory
func LoadTemplates(tt *Templates, p string) error {
	var err error

	// if tt.Dashboard, err = pc.ExtendTemplate(
	// 	tt.Templates.Dashboard,
	// 	fp.Join(p, "page/dashboard.html"),
	// ); err != nil {
	// 	return err
	// }

	funcs := t.FuncMap{
		"formatTimeStamp": func(timestamp int64) string {
			return time.Unix(timestamp, 0).Format("02 Jan 2006")
		},
		"formatCurrency": func(amount int64, currency stripe.Currency) string {
			return fmt.Sprintf("%.2f %s", float64(amount)/100.00, strings.ToUpper(string(currency)))
		},
	}

	if tt.Invoice, err = t.New("invoice.html.tmpl").Funcs(funcs).ParseFiles(fp.Join(p, "page/invoice.html.tmpl")); err != nil {
		return err
	}

	if tt.InvoiceList, err = t.New("invoice-list.html.tmpl").Funcs(funcs).ParseFiles(fp.Join(p, "page/invoice-list.html.tmpl")); err != nil {
		return err
	}

	return nil
}
