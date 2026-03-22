package wechat

import "time"

var shanghaiLoc *time.Location

func ShanghaiLoc() *time.Location {
	if shanghaiLoc == nil {
		loc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			loc = time.FixedZone("CST", 8*3600)
		}
		shanghaiLoc = loc
	}
	return shanghaiLoc
}

func FormatDate(t time.Time) string {
	return t.In(ShanghaiLoc()).Format("2006-01-02")
}

func ParseDate(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, ShanghaiLoc())
}

func Yesterday() string {
	return FormatDate(time.Now().In(ShanghaiLoc()).AddDate(0, 0, -1))
}

func AddDays(dateStr string, days int) (string, error) {
	t, err := ParseDate(dateStr)
	if err != nil {
		return "", err
	}
	return FormatDate(t.AddDate(0, 0, days)), nil
}

func GetDateRange(beginDate, endDate string) ([]string, error) {
	start, err := ParseDate(beginDate)
	if err != nil {
		return nil, err
	}
	end, err := ParseDate(endDate)
	if err != nil {
		return nil, err
	}

	var dates []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dates = append(dates, FormatDate(d))
	}
	return dates, nil
}
