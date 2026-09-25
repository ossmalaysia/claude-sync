package app

import (
	"errors"
	"time"
)

// TermsVersion identifies the first-use notice shown in the app. Change it
// when the notice's meaning changes, so everyone is asked again.
const TermsVersion = "1"

var ErrTermsNotAccepted = errors.New("please read and accept the notice first")

// AcceptTerms records that the user accepted the current first-use notice.
func (a *App) AcceptTerms() error {
	st, err := a.st.LoadSettings()
	if err != nil {
		return err
	}
	st.TermsVersion, st.TermsAcceptedAt = TermsVersion, time.Now().UTC()
	return a.st.SaveSettings(st)
}

func (a *App) termsAccepted() bool {
	st, err := a.st.LoadSettings()
	return err == nil && st.TermsVersion == TermsVersion
}
