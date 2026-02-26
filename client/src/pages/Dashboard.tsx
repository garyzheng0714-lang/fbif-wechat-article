import { Row, Col, Alert, Spin } from 'antd';
import {
  LineChartOutlined,
  PieChartOutlined,
  HeatMapOutlined,
} from '@ant-design/icons';
import type { DashboardData } from '../types/wechat';
import StatCards from '../components/cards/StatCards';
import ArticleTable from '../components/tables/ArticleTable';
import DailyTrendChart from '../components/charts/DailyTrendChart';
import TrafficSourceChart from '../components/charts/TrafficSourceChart';
import ShareSceneChart from '../components/charts/ShareSceneChart';
import HourlyHeatmapChart from '../components/charts/HourlyHeatmap';

interface DashboardProps {
  data: DashboardData | null;
  loading: boolean;
  error: string | null;
  showOnlyTable?: boolean;
}

function EmptyState({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <div className="glass-card">
      <div className="empty-state">
        <div className="empty-state-icon">{icon}</div>
        <div className="empty-state-text">{text}</div>
      </div>
    </div>
  );
}

export default function Dashboard({
  data,
  loading,
  error,
  showOnlyTable,
}: DashboardProps) {
  if (loading && !data) {
    return <Spin size="large" tip="加载数据中..." fullscreen />;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      {error && (
        <Alert
          message="数据加载失败"
          description={error}
          type="error"
          showIcon
          closable
          className="animate-in"
        />
      )}

      {showOnlyTable ? (
        <ArticleTable
          data={data?.articleSummary?.list || []}
          loading={loading}
        />
      ) : (
        <>
          <StatCards userReadData={data?.userRead?.list || []} />

          {data?.userRead?.list?.length ? (
            <DailyTrendChart data={data.userRead.list} />
          ) : (
            <EmptyState icon={<LineChartOutlined />} text="暂无阅读趋势数据" />
          )}

          <Row gutter={[20, 20]}>
            <Col xs={24} md={12}>
              {data?.userRead?.list?.length ? (
                <TrafficSourceChart data={data.userRead.list} />
              ) : (
                <EmptyState icon={<PieChartOutlined />} text="暂无流量来源数据" />
              )}
            </Col>
            <Col xs={24} md={12}>
              {data?.userShare?.list?.length ? (
                <ShareSceneChart data={data.userShare.list} />
              ) : (
                <EmptyState icon={<PieChartOutlined />} text="暂无分享场景数据" />
              )}
            </Col>
          </Row>

          {data?.userReadHour?.list?.length ? (
            <HourlyHeatmapChart data={data.userReadHour.list} />
          ) : (
            <EmptyState icon={<HeatMapOutlined />} text="暂无小时数据" />
          )}

          <ArticleTable
            data={data?.articleSummary?.list || []}
            loading={loading}
          />
        </>
      )}
    </div>
  );
}
