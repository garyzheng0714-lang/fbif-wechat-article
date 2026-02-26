import { useParams, useNavigate } from 'react-router-dom';
import { useEffect, useState, useCallback } from 'react';
import { Spin, Alert, Button, Row, Col } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { Line, Column } from '@ant-design/charts';
import { api } from '../api/wechat';
import type { ArticleTotalItem } from '../types/wechat';
import { formatNumber } from '../utils/format';

interface ArticleDetailProps {
  dateRange: [string, string];
}

export default function ArticleDetail({ dateRange }: ArticleDetailProps) {
  const { msgid } = useParams<{ msgid: string }>();
  const navigate = useNavigate();
  const [article, setArticle] = useState<ArticleTotalItem | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      setLoading(true);
      const result = await api.fetchArticleTotal(dateRange[0], dateRange[1]);
      const found = result.list.find(
        (a) => a.msgid === decodeURIComponent(msgid || ''),
      );
      if (found) {
        setArticle(found);
        setError(null);
      } else {
        setError('未找到该文章数据');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, [dateRange, msgid]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: 100 }}>
        <Spin size="large" />
      </div>
    );
  }

  if (error || !article) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(-1)}>
          返回
        </Button>
        <Alert message="加载失败" description={error} type="error" showIcon />
      </div>
    );
  }

  const details = article.details || [];
  const latestDetail = details[details.length - 1];

  const trendData = details.flatMap((d) => [
    { date: d.stat_date, value: d.int_page_read_count, type: '阅读量' },
    { date: d.stat_date, value: d.share_count, type: '分享数' },
    { date: d.stat_date, value: d.add_to_fav_count, type: '收藏数' },
  ]);

  const sourceData = latestDetail
    ? [
        { source: '公众号会话', count: latestDetail.int_page_from_session_read_count },
        { source: '历史消息', count: latestDetail.int_page_from_hist_msg_read_count },
        { source: '看一看', count: latestDetail.int_page_from_feed_read_count },
        { source: '好友转发', count: latestDetail.int_page_from_friends_read_count },
        { source: '其他', count: latestDetail.int_page_from_other_read_count },
      ].filter((d) => d.count > 0)
    : [];

  const darkAxis = {
    y: { title: '', labelFill: 'rgba(230,237,243,0.4)', gridStroke: 'rgba(255,255,255,0.04)' },
    x: { title: '', labelFill: 'rgba(230,237,243,0.4)' },
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <Button
        icon={<ArrowLeftOutlined />}
        onClick={() => navigate(-1)}
        style={{ alignSelf: 'flex-start' }}
      >
        返回
      </Button>

      {/* Article Header */}
      <div className="glass-card animate-in animate-in-1" style={{ padding: 28 }}>
        <h2 style={{
          fontFamily: 'var(--font-display)',
          fontSize: 22,
          fontWeight: 600,
          color: 'var(--text-primary)',
          margin: '0 0 16px 0',
          lineHeight: 1.4,
        }}>
          {article.title}
        </h2>
        <div style={{ display: 'flex', gap: 32, flexWrap: 'wrap' }}>
          <div>
            <div style={{ fontSize: 11, color: 'var(--text-tertiary)', marginBottom: 4, letterSpacing: '0.5px', textTransform: 'uppercase' }}>
              日期
            </div>
            <div style={{ fontFamily: 'var(--font-mono)', color: 'var(--text-secondary)' }}>
              {article.ref_date}
            </div>
          </div>
          {latestDetail && (
            <>
              <div>
                <div style={{ fontSize: 11, color: 'var(--text-tertiary)', marginBottom: 4, letterSpacing: '0.5px', textTransform: 'uppercase' }}>
                  送达人数
                </div>
                <div style={{ fontFamily: 'var(--font-mono)', color: 'var(--sky)', fontSize: 18 }}>
                  {formatNumber(latestDetail.target_user)}
                </div>
              </div>
              <div>
                <div style={{ fontSize: 11, color: 'var(--text-tertiary)', marginBottom: 4, letterSpacing: '0.5px', textTransform: 'uppercase' }}>
                  累计阅读
                </div>
                <div style={{ fontFamily: 'var(--font-mono)', color: 'var(--jade-200)', fontSize: 18 }}>
                  {formatNumber(latestDetail.int_page_read_count)}
                </div>
              </div>
              <div>
                <div style={{ fontSize: 11, color: 'var(--text-tertiary)', marginBottom: 4, letterSpacing: '0.5px', textTransform: 'uppercase' }}>
                  累计分享
                </div>
                <div style={{ fontFamily: 'var(--font-mono)', color: 'var(--amber)', fontSize: 18 }}>
                  {formatNumber(latestDetail.share_count)}
                </div>
              </div>
            </>
          )}
        </div>
      </div>

      {/* Cumulative Trend */}
      {trendData.length > 0 && (
        <div className="glass-card chart-card animate-in animate-in-3">
          <div className="chart-title">累计趋势 (发布后7天)</div>
          <Line
            data={trendData}
            xField="date"
            yField="value"
            colorField="type"
            smooth
            height={280}
            theme="classicDark"
            scale={{ color: { range: ['#07c160', '#58a6ff', '#f0b429'] } }}
            axis={darkAxis}
            legend={{ position: 'top', itemLabelFill: 'rgba(230,237,243,0.6)' }}
            interaction={{ tooltip: { marker: true } }}
            style={{ lineWidth: 2.5 }}
          />
        </div>
      )}

      {/* Source Breakdowns */}
      <Row gutter={[20, 20]}>
        <Col xs={24} md={12}>
          {sourceData.length > 0 && (
            <div className="glass-card chart-card animate-in animate-in-5">
              <div className="chart-title">流量来源分布</div>
              <Column
                data={sourceData}
                xField="source"
                yField="count"
                height={280}
                theme="classicDark"
                axis={darkAxis}
                style={{ fill: '#07c160', radiusTopLeft: 4, radiusTopRight: 4 }}
                interaction={{ tooltip: true }}
              />
            </div>
          )}
        </Col>
        <Col xs={24} md={12}>
          {latestDetail && (
            <div className="glass-card chart-card animate-in animate-in-5">
              <div className="chart-title">分享来源分布</div>
              <Column
                data={[
                  { source: '会话分享', count: latestDetail.feed_share_from_session_cnt },
                  { source: '看一看分享', count: latestDetail.feed_share_from_feed_cnt },
                  { source: '其他分享', count: latestDetail.feed_share_from_other_cnt },
                ].filter((d) => d.count > 0)}
                xField="source"
                yField="count"
                height={280}
                theme="classicDark"
                axis={darkAxis}
                style={{ fill: '#58a6ff', radiusTopLeft: 4, radiusTopRight: 4 }}
                interaction={{ tooltip: true }}
              />
            </div>
          )}
        </Col>
      </Row>
    </div>
  );
}
