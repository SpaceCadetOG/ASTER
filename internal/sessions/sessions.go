package sessions

import "time"

func mustTZ(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func inRange(localNow time.Time, startHM, endHM [2]int) bool {
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), startHM[0], startHM[1], 0, 0, localNow.Location())
	end := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), endHM[0], endHM[1], 0, 0, localNow.Location())
	return !localNow.Before(start) && !localNow.After(end)
}

func ActiveSessionLabels(nowUTC time.Time) []string {
	labels := []string{}
	// Asia (Tokyo, Singapore)
	tok := nowUTC.In(mustTZ("Asia/Tokyo"))
	sg := nowUTC.In(mustTZ("Asia/Singapore"))
	if inRange(tok, [2]int{9, 0}, [2]int{10, 30}) || inRange(sg, [2]int{9, 0}, [2]int{11, 0}) {
		labels = append(labels, "ASIA_OPEN")
	}
	// ASIA_EU_OVERLAP: London 07:00–09:00
	lon := nowUTC.In(mustTZ("Europe/London"))
	if inRange(lon, [2]int{7, 0}, [2]int{9, 0}) {
		labels = append(labels, "ASIA_EU_OVERLAP")
	}
	// LONDON_OPEN: London 08:00–10:00
	if inRange(lon, [2]int{8, 0}, [2]int{10, 0}) {
		labels = append(labels, "LONDON_OPEN")
	}
	// LONDON_NY_OVERLAP: London 13:30–16:00
	if inRange(lon, [2]int{13, 30}, [2]int{16, 0}) {
		labels = append(labels, "LONDON_NY_OVERLAP")
	}
	// NY_OPEN: New York 09:30–10:30
	ny := nowUTC.In(mustTZ("America/New_York"))
	if inRange(ny, [2]int{9, 30}, [2]int{10, 30}) {
		labels = append(labels, "NY_OPEN")
	}
	// NY_CLOSE: New York 15:30–16:15
	if inRange(ny, [2]int{15, 30}, [2]int{16, 15}) {
		labels = append(labels, "NY_CLOSE")
	}
	// Close labels (simple append at end if in-range)
	if inRange(sg, [2]int{10, 30}, [2]int{11, 0}) || inRange(tok, [2]int{10, 30}, [2]int{11, 0}) {
		labels = append(labels, "ASIA_CLOSE")
	}
	if inRange(lon, [2]int{9, 45}, [2]int{10, 15}) {
		labels = append(labels, "LONDON_CLOSE")
	}
	return labels
}
