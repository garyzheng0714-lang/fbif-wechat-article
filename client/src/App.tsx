import { useState } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { ConfigProvider, theme } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';
import './index.css';
import AppLayout from './components/layout/AppLayout';
import Dashboard from './pages/Dashboard';
import ArticleDetail from './pages/ArticleDetail';
import Settings from './pages/Settings';
import { useWechatData } from './hooks/useWechatData';

dayjs.locale('zh-cn');

const defaultRange: [string, string] = [
  dayjs().subtract(7, 'day').format('YYYY-MM-DD'),
  dayjs().subtract(1, 'day').format('YYYY-MM-DD'),
];

export default function App() {
  const [dateRange, setDateRange] = useState<[string, string]>(defaultRange);
  const [pollingEnabled, setPollingEnabled] = useState(true);
  const { data, loading, error, lastRefreshed } = useWechatData(
    dateRange,
    pollingEnabled,
  );

  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: theme.darkAlgorithm,
        token: {
          colorPrimary: '#07c160',
          colorBgContainer: '#161b22',
          colorBgElevated: '#1c2333',
          colorBorder: 'rgba(255,255,255,0.08)',
          colorBorderSecondary: 'rgba(255,255,255,0.06)',
          colorText: '#e6edf3',
          colorTextSecondary: 'rgba(230,237,243,0.6)',
          colorTextTertiary: 'rgba(230,237,243,0.35)',
          borderRadius: 10,
          fontFamily: "-apple-system, 'PingFang SC', 'Noto Sans SC', 'Microsoft YaHei', sans-serif",
        },
        components: {
          Table: {
            headerBg: '#1c2333',
            headerColor: 'rgba(230,237,243,0.6)',
            rowHoverBg: 'rgba(30,37,48,0.85)',
            borderColor: 'rgba(255,255,255,0.06)',
            colorBgContainer: 'transparent',
          },
          Menu: {
            darkItemBg: 'transparent',
            darkItemSelectedBg: 'rgba(7,193,96,0.15)',
            darkItemSelectedColor: '#6ee7a8',
            darkItemColor: 'rgba(230,237,243,0.6)',
            darkItemHoverColor: '#e6edf3',
            darkItemHoverBg: 'rgba(255,255,255,0.04)',
          },
        },
      }}
    >
      <BrowserRouter>
        <Routes>
          <Route
            element={
              <AppLayout
                dateRange={dateRange}
                onDateRangeChange={setDateRange}
                pollingEnabled={pollingEnabled}
                onPollingToggle={setPollingEnabled}
                lastRefreshed={lastRefreshed}
              />
            }
          >
            <Route
              index
              element={
                <Dashboard data={data} loading={loading} error={error} />
              }
            />
            <Route
              path="/articles"
              element={
                <Dashboard
                  data={data}
                  loading={loading}
                  error={error}
                  showOnlyTable
                />
              }
            />
            <Route
              path="/article/:msgid"
              element={<ArticleDetail dateRange={dateRange} />}
            />
            <Route path="/settings" element={<Settings />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ConfigProvider>
  );
}
