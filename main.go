package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image/color"
	"log"
	"strconv"
	"strings"
	"time"

	// _ "net/http/pprof"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/systray"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
)

var (
	a fyne.App
	w fyne.Window

	speakerInitialized      = false
	appName                 = ""
	timerText, reminderText *canvas.Text

	workSeconds             = 25 * 60 // use int instead of duration for lower CPU usage
	breakSeconds            = 5 * 60
	forceWindowFocusSeconds = 1 * 60
	working, breaking       bool
	forceWindowFocusRunning bool
	workRoundCountKey       = "workRoundCount"

	//go:embed notification.wav
	soundFile []byte
	//go:embed app-icon.png
	systrayIcon []byte
)

func main() {
	// go func() {
	// 	log.Println(http.ListenAndServe("localhost:6060", nil))
	// }()

	a = app.NewWithID("com.github.able8.work-break-timer")
	drv, _ := fyne.CurrentApp().Driver().(desktop.Driver)
	w = drv.CreateSplashWindow()

	// force focus on the window to have a break
	a.Lifecycle().SetOnExitedForeground(func() {
		if !forceWindowFocusRunning {
			forceWindowFocusRunning = true
			time.Sleep(time.Second * time.Duration(forceWindowFocusSeconds))
			if breaking {
				w.RequestFocus()
			}
			forceWindowFocusRunning = false
		}
	})

	if desk, ok := a.(desktop.App); ok {
		setupSystemTrayMenu(desk)
	}

	rectangle := canvas.NewRectangle(color.NRGBA{R: 86, G: 131, B: 131, A: 255})

	reminderText = canvas.NewText("Time for a break!", color.NRGBA{R: 206, G: 206, B: 206, A: 255})
	reminderText.TextSize = 80
	reminderText.TextStyle = fyne.TextStyle{Bold: true}
	reminderText.Alignment = fyne.TextAlignCenter

	timerText = canvas.NewText("", color.NRGBA{R: 206, G: 206, B: 206, A: 255})
	timerText.TextSize = 50
	timerText.TextStyle = fyne.TextStyle{Bold: true}
	timerText.Alignment = fyne.TextAlignCenter

	w.Resize(fyne.NewSize(1000, 600))
	w.SetContent(container.NewStack(rectangle, container.NewGridWithRows(2, container.NewBorder(nil, reminderText, nil, nil), container.NewBorder(timerText, nil, nil, nil))))
	// w.SetFullScreen(true) //  not work well

	go startWorkTimer()

	a.Run()
}

func makeMenu() *fyne.Menu {
	return fyne.NewMenu(appName,
		fyne.NewMenuItem("Enable", func() {
			startWorkTimer()
		}),
		fyne.NewMenuItem("Disable", func() {
			disableSystrayTimer()
			if working {
				go stopWorkTimer()
				return
			}

			if breaking {
				go stopBreakTimer()
				return
			}
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Settings...", func() {
			settings := NewSettings()
			settings.SetOnSubmit(func() {
				newPref := Load()
				workSeconds = newPref.workMinutes * 60
				breakSeconds = newPref.breakMinutes * 60
				forceWindowFocusSeconds = newPref.forceWindowFocusDuration
			})
			settings.Show()
		}),
	)
}

func setupSystemTrayMenu(desk desktop.App) {
	systemTrayIcon := fyne.NewStaticResource("icon", systrayIcon)
	desk.SetSystemTrayIcon(systemTrayIcon)

	menu := makeMenu()
	desk.SetSystemTrayMenu(menu)
}

func startWorkTimer() {
	duration := workSeconds
	working = true
	breaking = false

	a.SendNotification(fyne.NewNotification(fmt.Sprintf("No.%d Start Work Timer", getWorkRoundCounter()+1), "Start Work Timer"))
	w.Hide()

	for {
		if !working {
			return
		}
		updateSystrayTimer(workSeconds - duration)
		if duration == 0 {
			setWorkRoundCounter()
			stopWorkTimer()
			startBreakTimer()
			return
		}
		duration--
		time.Sleep(time.Second)
		// log.Println("tick", "Number of goroutines:", runtime.NumGoroutine())
	}
}

func stopWorkTimer() {
	working = false
	playSound()
}

func stopBreakTimer() {
	breaking = false
	w.Hide()
	playSound()
}

func startBreakTimer() {
	// log.Println("start break ticker")
	disableSystrayTimer()
	a.SendNotification(fyne.NewNotification("Start Break Timer", "Start Break Timer"))
	w.Content().Refresh()
	w.Show()

	duration := breakSeconds
	breaking = true
	for {
		if !breaking {
			return
		}
		if duration == 0 {
			stopBreakTimer()
			go startWorkTimer()
			return
		}
		timerText.Text = formatTime(duration)
		timerText.Refresh()
		duration--
		time.Sleep(time.Second)
		w.Content().Refresh()
	}
}

func updateSystrayTimer(d int) {
	str := formatTime(d)
	systray.SetTitle(appName + str)
	// systray.SetTooltip(fmt.Sprintf("%s%s", appName, str))
}

func disableSystrayTimer() {
	systray.SetTitle(appName)
	systray.SetTooltip(appName)
}

func formatTime(timer int) string {
	minutes := timer / 60
	seconds := timer % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func playSound() {
	// _, filepath, line, ok := runtime.Caller(1)
	// log.Println(filepath, line, ok)

	streamer, format, err := wav.Decode(bytes.NewReader(soundFile))
	if err != nil {
		log.Fatal("Unable to stream the notification sound")
	}
	defer streamer.Close()

	// activate speakers
	if !speakerInitialized {
		speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
		if err != nil {
			log.Fatal(err)
		}
		speakerInitialized = true
	}

	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))

	// Wait for the audio to finish playing
	<-done
}

func setWorkRoundCounter() {
	today := time.Now().Format("2006-01-02")
	counters := a.Preferences().StringList(workRoundCountKey)
	if len(counters) > 0 {
		fields := strings.Split(counters[len(counters)-1], ",")
		if fields[0] == today {
			workRoundCount, err := strconv.Atoi(fields[1])
			if err != nil {
				return
			}
			workRoundCount++
			counters[len(counters)-1] = fmt.Sprintf("%s,%d", today, workRoundCount)
		} else {
			counters = append(counters, fmt.Sprintf("%s,%d", today, 1))
		}
	} else {
		counters = append(counters, fmt.Sprintf("%s,%d", today, 1))
	}
	a.Preferences().SetStringList(workRoundCountKey, counters)
}

func getWorkRoundCounter() int {
	today := time.Now().Format("2006-01-02")
	counters := a.Preferences().StringList(workRoundCountKey)
	if len(counters) == 0 {
		return 0
	}

	s := counters[len(counters)-1]
	fields := strings.Split(s, ",")
	if fields[0] == today {
		workRoundCount, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0
		}
		return workRoundCount
	}
	return 0
}
