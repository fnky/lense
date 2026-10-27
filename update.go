package main

import (
	"context"
	"time"
)

// updater updates the app from its releases (mygo.Updater): enabled
// reports whether this copy can update itself, check finds a newer
// version, nil when the app is up to date. Run check in the background.
type updater interface {
	enabled() bool
	check(ctx context.Context) (availableUpdate, error)
}

// availableUpdate is a newer version of the app, which install downloads,
// checks and puts in place of the app, to run as it restarts. Run install
// in the background.
type availableUpdate interface {
	version() string
	install(ctx context.Context, progress func(done, total int64)) error
}

// updateState is where updating the app stands, as Settings shows it.
type updateState int

const (
	updateIdle        updateState = iota // not checked yet
	updateChecking                       // asking the releases
	updateUpToDate                       // none newer
	updateAvailable                      // a newer version, for the user to install
	updateDownloading                    // getting it
	updateReady                          // installed, for the restart
	updateFailed                         // why is in err
)

// softwareUpdate is how updating the app goes.
type softwareUpdate struct {
	state    updateState
	found    availableUpdate
	progress float64 // of the download, from 0 to 1
	failed   string  // what failed, and err why
	err      string
	checked  time.Time // when it last checked
}

// The time between automatic checks, and before the first after launch.
const (
	updateInterval   = 24 * time.Hour
	firstUpdateCheck = 10 * time.Second
)

// canUpdate reports whether this copy of the app updates itself.
func (a *app) canUpdate() bool { return a.upd != nil && a.updEnabled }

// busyUpdating reports whether a check or a download is under way, or an
// update waits for the restart: no other check starts then.
func (a *app) busyUpdating() bool {
	switch a.update.state {
	case updateChecking, updateDownloading, updateReady:
		return true
	}
	return false
}

// checkForUpdates looks for a newer version, as the user asked or as
// automatic updates do, which install it at once (install).
func (a *app) checkForUpdates(install bool) {
	if !a.canUpdate() || a.busyUpdating() {
		return
	}
	a.update.state, a.update.err = updateChecking, ""
	upd := a.upd
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		found, err := upd.check(ctx)
		a.post(func() {
			a.update.checked = a.now()
			switch {
			case err != nil:
				a.update.state, a.update.failed, a.update.err = updateFailed, "Couldn't check for updates", err.Error()
			case found == nil:
				a.update.state = updateUpToDate
			default:
				a.update.state, a.update.found = updateAvailable, found
				if install {
					a.installUpdate()
				}
			}
		})
	})
}

// installUpdate downloads the version found, which runs as the app
// restarts.
func (a *app) installUpdate() {
	found := a.update.found
	if found == nil || a.update.state != updateAvailable {
		return
	}
	a.update.state, a.update.progress = updateDownloading, 0
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err := found.install(ctx, func(done, total int64) {
			if total > 0 {
				p := float64(done) / float64(total)
				a.post(func() { a.update.progress = p })
			}
		})
		a.post(func() {
			if err != nil {
				a.update.state = updateFailed
				a.update.failed, a.update.err = "Couldn't download Lense "+found.version(), err.Error()
				return
			}
			a.update.state = updateReady
		})
	})
}

// autoUpdate checks for an update and installs it, when automatic
// updates are on: soon after launch, once a day, and as they are turned on.
func (a *app) autoUpdate() {
	if a.cfg.AutoUpdates {
		a.checkForUpdates(true)
	}
}

// restartToUpdate restarts the app into the update it installed.
func (a *app) restartToUpdate() {
	if a.update.state == updateReady && a.relaunch != nil {
		a.relaunch()
	}
}
