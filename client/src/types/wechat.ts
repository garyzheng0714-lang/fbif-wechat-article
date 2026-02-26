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

export interface UserReadHourItem extends UserReadItem {
  ref_hour: number;
}

export interface UserShareItem {
  ref_date: string;
  share_scene: number;
  share_user: number;
  share_count: number;
}

export interface UserShareHourItem extends UserShareItem {
  ref_hour: number;
}

export interface DashboardData {
  articleSummary: { list: ArticleSummaryItem[] } | null;
  articleTotal: { list: ArticleTotalItem[] } | null;
  userRead: { list: UserReadItem[] } | null;
  userReadHour: { list: UserReadHourItem[] } | null;
  userShare: { list: UserShareItem[] } | null;
  userShareHour: { list: UserShareHourItem[] } | null;
}
