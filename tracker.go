package main

import (
	"github.com/dukex/mixpanel"
	pc "github.com/maklesoft/padlock-cloud/padlockcloud"
	"github.com/satori/go.uuid"
	"net/http"
	"time"
)

func sourceFromRef(ref string) string {
	switch ref {
	case "app-1":
		return "Cloud View - Manage Account"
	case "app-2":
		return "Cloud View - Trialing"
	case "app-3":
		return "Cloud View - Readonly"
	case "app-4":
		return "Dialog - Trial Ending"
	case "app-5":
		return "Dialog - Readonly"
	case "pair":
		return "Device Paired"
	default:
		return ref
	}
}

type TrackingEvent struct {
	TrackingID string                 `json:"trackingID"`
	Name       string                 `json:"event"`
	Properties map[string]interface{} `json:"props"`
	request    *http.Request
	authToken  *pc.AuthToken
}

type Tracker interface {
	Track(event *TrackingEvent) error
	DeleteProfile(acc *Account) error
	UpdateProfile(acc *Account, props map[string]interface{}) error
	UnsubscribeProfile(tid string) error
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

func (t *mixpanelTracker) Track(event *TrackingEvent) error {
	var ip string
	if event.request != nil {
		ip = pc.IPFromRequest(event.request)
	}

	a := event.authToken
	originalTrackingID := event.TrackingID

	if event.TrackingID == "" {
		event.TrackingID = uuid.NewV4().String()
	}

	if event.Properties == nil {
		event.Properties = make(map[string]interface{})
	}

	var acc *Account
	if a != nil {
		_acc := &Account{Email: a.Email}
		if err := t.storage.Get(_acc); err == nil {
			acc = _acc
		}
	}

	if acc != nil {
		if acc.TrackingID == "" {
			acc.TrackingID = event.TrackingID
			t.storage.Put(acc)
		} else {
			event.TrackingID = acc.TrackingID

			if originalTrackingID != "" && originalTrackingID != acc.TrackingID {
				if err := t.mixpanel.Update(originalTrackingID, &mixpanel.Update{
					IP:        ip,
					Operation: "$set_once",
					Properties: map[string]interface{}{
						"Converted To": acc.TrackingID,
					},
				}); err != nil {
					return err
				}
			}
		}
	}

	props := event.Properties

	props["Authenticated"] = a != nil

	var device *pc.Device
	if a != nil {
		device = a.Device
	} else if event.request != nil {
		device = pc.DeviceFromRequest(event.request)
	}

	if device != nil {
		props["Platform"] = device.Platform
		props["Device UUID"] = device.UUID
		props["Device Manufacturer"] = device.Manufacturer
		props["Device Model"] = device.Model
		props["OS Version"] = device.OSVersion
		props["Device Name"] = device.HostName
		props["App Version"] = device.AppVersion
	}

	if err := t.mixpanel.Track(event.TrackingID, event.Name, &mixpanel.Event{
		IP:         ip,
		Properties: props,
	}); err != nil {
		return err
	}

	// If the user is logged in, update/create profile
	if acc != nil {

		updateOnce := map[string]interface{}{
			"$email":                        acc.Email,
			"Created Padlock Cloud Account": acc.Created.UTC().Format(time.RFC3339),
			"$created":                      props["First Launch"],
			"First App Launch":              props["First Launch"],
			"First Platform":                props["Platform"],
		}

		if err := t.mixpanel.Update(event.TrackingID, &mixpanel.Update{
			IP:         ip,
			Operation:  "$set_once",
			Properties: updateOnce,
		}); err != nil {
			return err
		}

		nDevices := 0
		platforms := make([]string, 0)
		versions := make([]string, 0)
		pMap := make(map[string]bool)
		vMap := make(map[string]bool)
		for _, token := range a.Account().AuthTokens {
			if token.Type == "api" && !token.Expired() {
				nDevices = nDevices + 1
			}
			if token.Device != nil && token.Device.Platform != "" && !pMap[token.Device.Platform] {
				platforms = append(platforms, token.Device.Platform)
				pMap[token.Device.Platform] = true
			}
			if token.Device != nil && token.Device.AppVersion != "" && !vMap[token.Device.AppVersion] {
				versions = append(versions, token.Device.AppVersion)
				vMap[token.Device.AppVersion] = true
			}
		}

		if err := t.UpdateProfile(acc, map[string]interface{}{
			"Last Sync":      props["Last Sync"],
			"Last Rated":     props["Last Rated"],
			"Rated Version":  props["Rated Version"],
			"Rating":         props["Rating"],
			"Last Reviewed":  props["Last Reviewed"],
			"Paired Devices": nDevices,
			"Platforms":      platforms,
			"Versions":       versions,
		}); err != nil {
			return err
		}

	}

	return nil
}

func (t *mixpanelTracker) UpdateProfile(acc *Account, props map[string]interface{}) error {
	if acc.TrackingID == "" {
		acc.TrackingID = uuid.NewV4().String()
		t.storage.Put(acc)
	}

	subStatus, _ := acc.SubscriptionStatus()

	update := map[string]interface{}{
		"Last Updated":        time.Now().UTC().Format(time.RFC3339),
		"Plan":                acc.SubscriptionPlan(),
		"Subscription Status": subStatus,
	}

	if props != nil {
		for k, v := range props {
			update[k] = v
		}
	}

	return t.mixpanel.Update(acc.TrackingID, &mixpanel.Update{
		IP:         "0",
		Operation:  "$set",
		Timestamp:  mixpanel.IgnoreTime,
		Properties: update,
	})

}

func (t *mixpanelTracker) DeleteProfile(acc *Account) error {
	if acc.TrackingID == "" {
		return nil
	}

	if err := t.Track(&TrackingEvent{
		TrackingID: acc.TrackingID,
		Name:       "Delete Account",
	}); err != nil {
		return err
	}

	return t.mixpanel.Update(acc.TrackingID, &mixpanel.Update{
		Operation: "$set",
		Properties: map[string]interface{}{
			"Account Deleted": time.Now().UTC().Format(time.RFC3339),
			"$email":          "",
		},
	})
}

func (t *mixpanelTracker) UnsubscribeProfile(tid string) error {
	if err := t.Track(&TrackingEvent{
		TrackingID: tid,
		Name:       "Unsubscribe",
	}); err != nil {
		return err
	}

	return t.mixpanel.Update(tid, &mixpanel.Update{
		Operation: "$set",
		Properties: map[string]interface{}{
			"$unsubscribed": time.Now().UTC().Format(time.RFC3339),
		},
	})
}
