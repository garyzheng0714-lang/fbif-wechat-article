# 微信公众号数据分析 API 官方文档

> 来源: https://developers.weixin.qq.com/doc/subscription/api/wedata/news/

---

## 1. 获取图文群发每日数据 (getarticlesummary)

**接口地址:** `POST https://api.weixin.qq.com/datacube/getarticlesummary?access_token=ACCESS_TOKEN`

**最大跨度:** 1天

### 请求参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| access_token | string | 是 | 接口调用凭证 |
| begin_date | string | 是 | 开始日期，格式 YYYY-MM-DD |
| end_date | string | 是 | 结束日期，最大值为昨天 |

### 返回字段 (list 数组)

| 字段 | 类型 | 说明 |
|------|------|------|
| ref_date | string | 数据日期 |
| msgid | string | 消息ID，由 msgid 和 index 组成（如 12003_3） |
| title | string | 文章标题 |
| int_page_read_user | number | 图文页阅读人数 |
| int_page_read_count | number | 图文页阅读次数 |
| ori_page_read_user | number | 原文页阅读人数 |
| ori_page_read_count | number | 原文页阅读次数 |
| share_user | number | 分享人数 |
| share_count | number | 分享次数 |
| add_to_fav_user | number | 收藏人数 |
| add_to_fav_count | number | 收藏次数 |

### 请求示例
```json
{
  "begin_date": "2014-12-08",
  "end_date": "2014-12-08"
}
```

### 返回示例
```json
{
  "list": [
    {
      "ref_date": "2014-12-08",
      "msgid": "10000050_1",
      "title": "12月27日 DiLi日报",
      "int_page_read_user": 23676,
      "int_page_read_count": 25615,
      "ori_page_read_count": 34,
      "share_user": 122,
      "share_count": 994,
      "add_to_fav_user": 1
    }
  ]
}
```

---

## 2. 获取图文群发总数据 (getarticletotal)

**接口地址:** `POST https://api.weixin.qq.com/datacube/getarticletotal?access_token=ACCESS_TOKEN`

**最大跨度:** 1天

### 请求参数

同上 (access_token, begin_date, end_date)

### 返回字段 (list 数组)

| 字段 | 类型 | 说明 |
|------|------|------|
| ref_date | string | 数据日期 |
| msgid | string | 消息ID |
| title | string | 文章标题 |
| details | array | 每日详细统计（从发布日到统计日，最多7天） |

### details 数组字段

| 字段 | 类型 | 说明 |
|------|------|------|
| stat_date | string | 统计日期 |
| target_user | number | 送达人数（约等于总粉丝数） |
| int_page_read_user | number | 图文页阅读人数 |
| int_page_read_count | number | 图文页阅读次数 |
| ori_page_read_user | number | 原文页阅读人数 |
| ori_page_read_count | number | 原文页阅读次数 |
| share_user | number | 分享人数 |
| share_count | number | 分享次数 |
| add_to_fav_user | number | 收藏人数 |
| add_to_fav_count | number | 收藏次数 |
| feed_share_from_session_cnt | number | 公众号会话转发到朋友圈的次数 |
| int_page_from_kanyikan_read_user | number | 看一看来源阅读人数 |
| int_page_from_souyisou_read_user | number | 搜一搜来源阅读人数 |

---

## 3. 获取图文统计数据 (getuserread)

**接口地址:** `POST https://api.weixin.qq.com/datacube/getuserread?access_token=ACCESS_TOKEN`

**最大跨度:** 3天

### 返回字段 (list 数组)

| 字段 | 类型 | 说明 |
|------|------|------|
| ref_date | string | 数据日期 |
| user_source | number | 用户来源（99999999=全部, 0=会话, 1=好友, 2=朋友圈, 4=历史消息, 5=其他, 6=看一看, 7=搜一搜） |
| int_page_read_user | number | 图文页阅读人数 |
| int_page_read_count | number | 图文页阅读次数 |
| ori_page_read_user | number | 原文页阅读人数 |
| ori_page_read_count | number | 原文页阅读次数 |
| share_user | number | 分享人数 |
| share_count | number | 分享次数 |
| add_to_fav_user | number | 收藏人数 |
| add_to_fav_count | number | 收藏次数 |

---

## 4. 获取图文分享转发数据 (getusershare)

**接口地址:** `POST https://api.weixin.qq.com/datacube/getusershare?access_token=ACCESS_TOKEN`

**最大跨度:** 7天

### 返回字段 (list 数组)

| 字段 | 类型 | 说明 |
|------|------|------|
| ref_date | string | 数据日期 |
| share_scene | number | 分享场景（1=好友转发, 2=朋友圈, 255=其他） |
| share_user | number | 分享人数 |
| share_count | number | 分享次数 |

---

## 通用错误码

| 错误码 | 说明 | 解决方案 |
|--------|------|----------|
| -1 | 系统繁忙 | 稍后重试 |
| 0 | 请求成功 | - |
| 40001 | access_token 无效 | 检查 AppSecret 是否正确，或 token 是否过期 |
| 61500 | 日期格式错误 | 使用 YYYY-MM-DD 格式 |
| 61501 | 日期范围错误 | 确保日期范围有效 |
| 61503 | 数据未就绪 | 稍后重试 |

---

## Access Token 获取

**接口地址:** `GET https://api.weixin.qq.com/cgi-bin/token?grant_type=client_credential&appid=APPID&secret=APPSECRET`

### 返回字段

| 字段 | 类型 | 说明 |
|------|------|------|
| access_token | string | 接口调用凭证 |
| expires_in | number | 有效期（秒），通常为 7200 |

### 注意事项
- access_token 有效期为 2 小时
- 需要服务器 IP 在微信公众号后台的 IP 白名单中
- AppID 和 AppSecret 可在微信公众号后台「开发 > 基本配置」中获取
