import { DatePicker } from 'antd';
import dayjs, { type Dayjs } from 'dayjs';

const { RangePicker } = DatePicker;

interface DateRangePickerProps {
  value: [string, string];
  onChange: (range: [string, string]) => void;
}

const presets: { label: string; value: [Dayjs, Dayjs] }[] = [
  { label: '昨日', value: [dayjs().subtract(1, 'day'), dayjs().subtract(1, 'day')] },
  { label: '近7天', value: [dayjs().subtract(7, 'day'), dayjs().subtract(1, 'day')] },
  { label: '近30天', value: [dayjs().subtract(30, 'day'), dayjs().subtract(1, 'day')] },
];

export default function DateRangePickerComponent({ value, onChange }: DateRangePickerProps) {
  const disabledDate = (current: Dayjs) => {
    return current && current >= dayjs().startOf('day');
  };

  return (
    <RangePicker
      value={[dayjs(value[0]), dayjs(value[1])]}
      onChange={(dates) => {
        if (dates && dates[0] && dates[1]) {
          onChange([dates[0].format('YYYY-MM-DD'), dates[1].format('YYYY-MM-DD')]);
        }
      }}
      disabledDate={disabledDate}
      presets={presets}
      allowClear={false}
    />
  );
}
