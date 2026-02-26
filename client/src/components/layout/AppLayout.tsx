import { useState } from 'react';
import { Layout, Menu, Switch, Space } from 'antd';
import {
  DashboardOutlined,
  FileTextOutlined,
  SettingOutlined,
} from '@ant-design/icons';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import DateRangePicker from './DateRangePicker';
import { formatTime } from '../../utils/format';

const { Header, Sider, Content } = Layout;

interface AppLayoutProps {
  dateRange: [string, string];
  onDateRangeChange: (range: [string, string]) => void;
  pollingEnabled: boolean;
  onPollingToggle: (enabled: boolean) => void;
  lastRefreshed: Date | null;
}

export default function AppLayout({
  dateRange,
  onDateRangeChange,
  pollingEnabled,
  onPollingToggle,
  lastRefreshed,
}: AppLayoutProps) {
  const [collapsed, setCollapsed] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();

  const menuItems = [
    {
      key: '/',
      icon: <DashboardOutlined />,
      label: '数据概览',
    },
    {
      key: '/articles',
      icon: <FileTextOutlined />,
      label: '文章列表',
    },
    {
      key: '/settings',
      icon: <SettingOutlined />,
      label: '配置',
    },
  ];

  const selectedKey =
    location.pathname === '/articles' ? '/articles'
    : location.pathname === '/settings' ? '/settings'
    : '/';

  return (
    <Layout style={{ minHeight: '100vh', background: 'var(--bg-primary)' }}>
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={setCollapsed}
        theme="dark"
        width={220}
        collapsedWidth={64}
        className="app-sidebar"
        style={{ background: 'var(--bg-sidebar)' }}
      >
        <div className="sidebar-brand">
          <div className="sidebar-brand-icon">F</div>
          {!collapsed && <div className="sidebar-brand-text">公众号数据</div>}
        </div>
        <div style={{ padding: '12px 0' }}>
          <Menu
            mode="inline"
            selectedKeys={[selectedKey]}
            items={menuItems}
            onClick={({ key }) => navigate(key)}
          />
        </div>
      </Sider>
      <Layout style={{ background: 'var(--bg-primary)' }}>
        <Header className="app-header">
          <DateRangePicker value={dateRange} onChange={onDateRangeChange} />
          <div className="header-right">
            <Space size={8} align="center">
              <Switch
                size="small"
                checked={pollingEnabled}
                onChange={onPollingToggle}
              />
              <span style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>
                自动刷新
              </span>
            </Space>
            <div className="refresh-indicator">
              <div className={`refresh-dot ${pollingEnabled ? '' : 'refresh-dot--off'}`} />
              {lastRefreshed && (
                <span>{formatTime(lastRefreshed)}</span>
              )}
            </div>
          </div>
        </Header>
        <Content className="page-content">
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
