package wechat

// --- getarticlesummary response ---

type ArticleSummaryItem struct {
	RefDate          string `json:"ref_date"`
	MsgID            string `json:"msgid"`
	Title            string `json:"title"`
	IntPageReadUser  int    `json:"int_page_read_user"`
	IntPageReadCount int    `json:"int_page_read_count"`
	OriPageReadUser  int    `json:"ori_page_read_user"`
	OriPageReadCount int    `json:"ori_page_read_count"`
	ShareUser        int    `json:"share_user"`
	ShareCount       int    `json:"share_count"`
	AddToFavUser     int    `json:"add_to_fav_user"`
	AddToFavCount    int    `json:"add_to_fav_count"`
}

// --- getarticletotal response ---

type ArticleTotalDetail struct {
	StatDate                     string `json:"stat_date"`
	TargetUser                   int    `json:"target_user"`
	IntPageReadUser              int    `json:"int_page_read_user"`
	IntPageReadCount             int    `json:"int_page_read_count"`
	OriPageReadUser              int    `json:"ori_page_read_user"`
	OriPageReadCount             int    `json:"ori_page_read_count"`
	ShareUser                    int    `json:"share_user"`
	ShareCount                   int    `json:"share_count"`
	AddToFavUser                 int    `json:"add_to_fav_user"`
	AddToFavCount                int    `json:"add_to_fav_count"`
	IntPageFromSessionReadUser   int    `json:"int_page_from_session_read_user"`
	IntPageFromSessionReadCount  int    `json:"int_page_from_session_read_count"`
	IntPageFromHistMsgReadUser   int    `json:"int_page_from_hist_msg_read_user"`
	IntPageFromHistMsgReadCount  int    `json:"int_page_from_hist_msg_read_count"`
	IntPageFromFeedReadUser      int    `json:"int_page_from_feed_read_user"`
	IntPageFromFeedReadCount     int    `json:"int_page_from_feed_read_count"`
	IntPageFromFriendsReadUser   int    `json:"int_page_from_friends_read_user"`
	IntPageFromFriendsReadCount  int    `json:"int_page_from_friends_read_count"`
	IntPageFromOtherReadUser     int    `json:"int_page_from_other_read_user"`
	IntPageFromOtherReadCount    int    `json:"int_page_from_other_read_count"`
	FeedShareFromSessionUser     int    `json:"feed_share_from_session_user"`
	FeedShareFromSessionCnt      int    `json:"feed_share_from_session_cnt"`
	FeedShareFromFeedUser        int    `json:"feed_share_from_feed_user"`
	FeedShareFromFeedCnt         int    `json:"feed_share_from_feed_cnt"`
	FeedShareFromOtherUser           int `json:"feed_share_from_other_user"`
	FeedShareFromOtherCnt            int `json:"feed_share_from_other_cnt"`
	IntPageFromKanyikanReadUser      int `json:"int_page_from_kanyikan_read_user"`
	IntPageFromSouyisouReadUser      int `json:"int_page_from_souyisou_read_user"`
}

type ArticleTotalItem struct {
	RefDate    string               `json:"ref_date"`
	MsgID      string               `json:"msgid"`
	Title      string               `json:"title"`
	UserSource int                  `json:"user_source"`
	URL        string               `json:"url"`
	ContentURL string               `json:"content_url"` // some account types use this field name
	Details    []ArticleTotalDetail `json:"details"`
}

// --- getusersummary response ---

type UserSummaryItem struct {
	RefDate    string `json:"ref_date"`
	UserSource int    `json:"user_source"`
	NewUser    int    `json:"new_user"`
	CancelUser int    `json:"cancel_user"`
}

// --- getusercumulate response ---

type UserCumulateItem struct {
	RefDate      string `json:"ref_date"`
	UserSource   int    `json:"user_source"`
	CumulateUser int    `json:"cumulate_user"`
}

// --- getuserread response ---

type UserReadItem struct {
	RefDate          string `json:"ref_date"`
	UserSource       int    `json:"user_source"`
	IntPageReadUser  int    `json:"int_page_read_user"`
	IntPageReadCount int    `json:"int_page_read_count"`
	OriPageReadUser  int    `json:"ori_page_read_user"`
	OriPageReadCount int    `json:"ori_page_read_count"`
	ShareUser        int    `json:"share_user"`
	ShareCount       int    `json:"share_count"`
	AddToFavUser     int    `json:"add_to_fav_user"`
	AddToFavCount    int    `json:"add_to_fav_count"`
}

// --- getusershare response ---

type UserShareItem struct {
	RefDate    string `json:"ref_date"`
	ShareScene int    `json:"share_scene"`
	ShareUser  int    `json:"share_user"`
	ShareCount int    `json:"share_count"`
}
