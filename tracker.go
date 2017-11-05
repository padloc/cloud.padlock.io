package main

import (
	"github.com/dukex/mixpanel"
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"github.com/satori/go.uuid"
	"net/http"
	"time"
)

type TrackingEvent struct {
	TrackingID string                 `json:"trackingID"`
	Name       string                 `json:"event"`
	Properties map[string]interface{} `json:"props"`
}

type Tracker interface {
	Track(event *TrackingEvent, r *http.Request, a *pc.AuthToken) error
}

type mixpanelTracker struct {
	mixpanel mixpanel.Mixpanel
	storage  pc.Storage
}

func NewMixpanelTracker(token string, storage pc.Storage) Tracker {
	return &mixpanelTracker{
		storage:  storage,
		mixpanel: mixpanel.New(token, ""),
	}
}

func (t *mixpanelTracker) Track(event *TrackingEvent, r *http.Request, a *pc.AuthToken) error {
	ip := pc.IPFromRequest(r)

	if event.TrackingID == "" {
		event.TrackingID = uuid.NewV4().String()
	}

	var acc *Account
	if a != nil {
		acc, _ = AccountFromEmail(a.Email, false, t.storage)
	}

	if acc != nil {
		if acc.TrackingID == "" {
			acc.TrackingID = event.TrackingID
			t.storage.Put(acc)
		} else {
			event.TrackingID = acc.TrackingID
		}
	}

	props := event.Properties

	device := pc.DeviceFromRequest(r)

	props["Platform"] = device.Platform
	props["Device UUID"] = device.UUID
	props["Device Manufacturer"] = device.Manufacturer
	props["Device Model"] = device.Model
	props["OS Version"] = device.OSVersion
	props["Device Name"] = device.HostName
	props["App Version"] = device.AppVersion
	props["Authenticated"] = a != nil

	if acc != nil {
		subStatus := "inactive"
		if s := acc.Subscription(); s != nil {
			subStatus = string(s.Status)
			props["Plan"] = s.Plan.Name
		}
		props["Subscription Status"] = subStatus
	}

	if err := t.mixpanel.Track(event.TrackingID, event.Name, &mixpanel.Event{
		IP:         ip,
		Properties: props,
	}); err != nil {
		return err
	}

	updateProps := map[string]interface{}{
		"$created":         props["First Launch"],
		"First App Launch": props["First Launch"],
		"First Platform":   props["Platform"],
	}

	if acc != nil {
		updateProps["$email"] = acc.Email
		updateProps["Created Padlock Cloud Account"] = acc.Created.UTC().Format(time.RFC3339)
	}

	if err := t.mixpanel.Update(event.TrackingID, &mixpanel.Update{
		IP:         ip,
		Operation:  "$set_once",
		Properties: updateProps,
	}); err != nil {
		return err
	}

	if a != nil {
		nDevices := 0
		platforms := make([]string, 0)
		pMap := make(map[string]bool)
		for _, token := range a.Account().AuthTokens {
			if token.Type == "api" && !token.Expired() {
				nDevices = nDevices + 1
			}
			if token.Device != nil && token.Device.Platform != "" && !pMap[token.Device.Platform] {
				platforms = append(platforms, token.Device.Platform)
				pMap[token.Device.Platform] = true
			}
		}

		updateProps = map[string]interface{}{
			"Paired Devices":      nDevices,
			"Platforms":           platforms,
			"Last Sync":           props["Last Sync"],
			"Subscription Status": props["Subscription Status"],
			"Plan":                props["Plan"],
		}
	} else {
		updateProps = make(map[string]interface{})
	}

	updateProps["Last Rated"] = props["Last Rated"]
	updateProps["Rated Version"] = props["Rated Version"]
	updateProps["Rating"] = props["Rating"]
	updateProps["Last Reviewed"] = props["Last Reviewed"]

	if err := t.mixpanel.Update(event.TrackingID, &mixpanel.Update{
		IP:         ip,
		Operation:  "$set",
		Properties: updateProps,
	}); err != nil {
		return err
	}

	return nil
}
