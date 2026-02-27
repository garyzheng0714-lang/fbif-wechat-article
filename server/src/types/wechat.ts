// --- getarticlesummary response ---
export interface ArticleSummaryItem {
  ref_date: string;
  msgid: string;
  title: string;
  int_page_read_user: number;
  int_page_read_count: number;
  ori_page_read_user: number;
  ori_page_read_count: number;
  share_user: number;
  share_count: number;
  add_to_fav_user: number;
  add_to_fav_count: number;
}

// --- getarticletotal response ---
export interface ArticleTotalDetail {
  stat_date: string;
  target_user: number;
  int_page_read_user: number;
  int_page_read_count: number;
  ori_page_read_user: number;
  ori_page_read_count: number;
  share_user: number;
  share_count: number;
  add_to_fav_user: number;
  add_to_fav_count: number;
  int_page_from_session_read_user: number;
  int_page_from_session_read_count: number;
  int_page_from_hist_msg_read_user: number;
  int_page_from_hist_msg_read_count: number;
  int_page_from_feed_read_user: number;
  int_page_from_feed_read_count: number;
  int_page_from_friends_read_user: number;
  int_page_from_friends_read_count: number;
  int_page_from_other_read_user: number;
  int_page_from_other_read_count: number;
  feed_share_from_session_user: number;
  feed_share_from_session_cnt: number;
  feed_share_from_feed_user: number;
  feed_share_from_feed_cnt: number;
  feed_share_from_other_user: number;
  feed_share_from_other_cnt: number;
}

export interface ArticleTotalItem {
  ref_date: string;
  msgid: string;
  title: string;
  details: ArticleTotalDetail[];
}

// --- getuserread response ---
export interface UserReadItem {
  ref_date: string;
  user_source: number;
  int_page_read_user: number;
  int_page_read_count: number;
  ori_page_read_user: number;
  ori_page_read_count: number;
  share_user: number;
  share_count: number;
  add_to_fav_user: number;
  add_to_fav_count: number;
}

// --- getuserreadhour response ---
export interface UserReadHourItem extends UserReadItem {
  ref_hour: number;
}

// --- getusershare response ---
export interface UserShareItem {
  ref_date: string;
  share_scene: number;
  share_user: number;
  share_count: number;
}

// --- getusersharehour response ---
export interface UserShareHourItem extends UserShareItem {
  ref_hour: number;
}

// --- getusersummary response ---
export interface UserSummaryItem {
  ref_date: string;
  user_source: number;
  new_user: number;
  cancel_user: number;
}

// --- getusercumulate response ---
export interface UserCumulateItem {
  ref_date: string;
  cumulate_user: number;
}

// --- Batch dashboard response ---
export interface DashboardData {
  articleSummary: { list: ArticleSummaryItem[] } | null;
  articleTotal: { list: ArticleTotalItem[] } | null;
  userRead: { list: UserReadItem[] } | null;
  userReadHour: { list: UserReadHourItem[] } | null;
  userShare: { list: UserShareItem[] } | null;
  userShareHour: { list: UserShareHourItem[] } | null;
}
