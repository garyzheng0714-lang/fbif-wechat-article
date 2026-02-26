// user_source labels for getuserread / getuserreadhour
export const USER_SOURCE_LABELS: Record<number, string> = {
  0: '公众号会话',
  1: '好友转发',
  2: '朋友圈',
  4: '历史消息',
  5: '其他',
  6: '看一看',
  7: '搜一搜',
  99999999: '全部',
};

// share_scene labels for getusershare / getusersharehour
export const SHARE_SCENE_LABELS: Record<number, string> = {
  1: '好友转发',
  2: '朋友圈',
  255: '其他',
};

// Colors for charts
export const SOURCE_COLORS = [
  '#1677ff', // 公众号会话
  '#52c41a', // 好友转发
  '#faad14', // 朋友圈
  '#722ed1', // 历史消息
  '#8c8c8c', // 其他
  '#13c2c2', // 看一看
  '#eb2f96', // 搜一搜
];

export const SHARE_COLORS = ['#1677ff', '#52c41a', '#8c8c8c'];
