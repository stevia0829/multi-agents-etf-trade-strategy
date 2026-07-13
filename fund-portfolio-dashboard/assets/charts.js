(function() {
  var style = getComputedStyle(document.documentElement);
  var accent = style.getPropertyValue('--accent').trim();
  var accent2 = style.getPropertyValue('--accent2').trim();
  var ink = style.getPropertyValue('--ink').trim();
  var muted = style.getPropertyValue('--muted').trim();
  var rule = style.getPropertyValue('--rule').trim();
  var bg2 = style.getPropertyValue('--bg2').trim();
  var up = style.getPropertyValue('--up').trim();
  var down = style.getPropertyValue('--down').trim();
  var warn = style.getPropertyValue('--warn').trim();

  // ===== 资产配置饼图 =====
  var chartAlloc = echarts.init(document.getElementById('chart-allocation'), null, { renderer: 'svg' });
  chartAlloc.setOption({
    animation: false,
    tooltip: { trigger: 'item', appendToBody: true, formatter: '{b}<br/>¥{c} ({d}%)' },
    legend: {
      orient: 'vertical', right: '5%', top: 'center',
      textStyle: { color: muted, fontSize: 12 },
      itemWidth: 12, itemHeight: 12
    },
    series: [{
      type: 'pie',
      radius: ['40%', '70%'],
      center: ['38%', '50%'],
      label: { show: false },
      labelLine: { show: false },
      data: [
        { value: 130076, name: '固收+底仓', itemStyle: { color: '#5B8DEF' } },
        { value: 162961, name: 'A股宽基', itemStyle: { color: accent } },
        { value: 71072, name: 'A股行业/主题', itemStyle: { color: accent2 } },
        { value: 31381, name: 'QDII海外', itemStyle: { color: '#E8A838' } },
        { value: 59000, name: '现金（待配置）', itemStyle: { color: '#6B7B8E' } }
      ]
    }]
  });
  window.addEventListener('resize', function() { chartAlloc.resize(); });

  // ===== 风格暴露雷达图 =====
  var chartRadar = echarts.init(document.getElementById('chart-radar'), null, { renderer: 'svg' });
  chartRadar.setOption({
    animation: false,
    tooltip: { appendToBody: true },
    radar: {
      indicator: [
        { name: '大盘价值', max: 50 },
        { name: '大盘成长', max: 50 },
        { name: '小盘成长', max: 50 },
        { name: '科技/半导体', max: 50 },
        { name: '海外配置', max: 50 },
        { name: '固收防御', max: 50 }
      ],
      axisName: { color: ink, fontSize: 12 },
      splitLine: { lineStyle: { color: rule } },
      splitArea: { areaStyle: { color: ['transparent', bg2] } },
      axisLine: { lineStyle: { color: rule } }
    },
    series: [{
      type: 'radar',
      data: [
        {
          value: [30, 12, 17, 9, 10, 32],
          name: '当前暴露',
          areaStyle: { color: accent + '33' },
          lineStyle: { color: accent, width: 2 },
          itemStyle: { color: accent }
        },
        {
          value: [22, 10, 10, 8, 18, 32],
          name: '目标暴露',
          areaStyle: { color: accent2 + '22' },
          lineStyle: { color: accent2, width: 2, type: 'dashed' },
          itemStyle: { color: accent2 }
        }
      ]
    }],
    legend: {
      data: ['当前暴露', '目标暴露'],
      bottom: 0,
      textStyle: { color: muted, fontSize: 11 }
    }
  });
  window.addEventListener('resize', function() { chartRadar.resize(); });

  // ===== 动量诊断柱状图 =====
  var chartMomentum = echarts.init(document.getElementById('chart-momentum'), null, { renderer: 'svg' });
  var fundNames = ['沪深300\n增强', '中证1000\n量化(减半)', '中证500\n增强', '中证A50\n增强', '科创创业\n50增强', '集成电路\n(止损)', '互联科技\nA', '半导体\n(止盈半)', '高端制造', '科鑫量化\n(清仓)', '蓝筹精选', '纳指100\n联接', '全球成长', '全球高端\n制造', '亚洲机会', '新兴市场\n(清仓)'];
  var momentumScores = [1.2, -0.8, -1.5, 0.5, 3.8, -2.1, 1.8, 4.2, 1.5, -1.0, 0.8, 2.5, 3.2, 1.0, 1.3, -0.5];
  var barColors = momentumScores.map(function(v) {
    if (v >= 2.5) return up;
    if (v >= 0.5) return accent;
    if (v >= -1) return warn;
    return down;
  });
  chartMomentum.setOption({
    animation: false,
    tooltip: {
      trigger: 'axis', appendToBody: true,
      formatter: function(params) {
        var p = params[0];
        var label = p.name.replace('\n', ' ');
        var status = '';
        if (p.value >= 2.5) status = '🟢 强势动量';
        else if (p.value >= 0.5) status = '🟡 温和向上';
        else if (p.value >= -1) status = '🟠 动量走弱';
        else status = '🔴 动量恶化';
        return label + '<br/>动量分: ' + p.value.toFixed(1) + '<br/>' + status;
      }
    },
    grid: { left: '3%', right: '4%', bottom: '15%', top: '5%', containLabel: true },
    xAxis: {
      type: 'category',
      data: fundNames,
      axisLabel: { color: muted, fontSize: 9, interval: 0 },
      axisLine: { lineStyle: { color: rule } }
    },
    yAxis: {
      type: 'value',
      name: '动量分', nameTextStyle: { color: muted, fontSize: 10 },
      axisLabel: { color: muted, fontSize: 10 },
      splitLine: { lineStyle: { color: rule } },
      axisLine: { lineStyle: { color: rule } }
    },
    series: [{
      type: 'bar',
      data: momentumScores.map(function(v, i) {
        return { value: v, itemStyle: { color: barColors[i], borderRadius: [3, 3, 0, 0] } };
      }),
      barWidth: '55%',
      markLine: {
        data: [{ yAxis: 0 }],
        lineStyle: { color: rule, type: 'solid' },
        label: { show: false }
      }
    }]
  });
  window.addEventListener('resize', function() { chartMomentum.resize(); });

  // ===== 收益分布柱状图 =====
  var chartReturns = echarts.init(document.getElementById('chart-returns'), null, { renderer: 'svg' });
  var returnsData = [
    { name: '德邦半导体(止盈)', value: 61.89 },
    { name: '全球成长', value: 48.75 },
    { name: '纳指100联接', value: 29.08 },
    { name: '亚洲机会', value: 14.58 },
    { name: '高端制造', value: 13.41 },
    { name: '互联科技A', value: 6.97 },
    { name: '科创创业50', value: 6.27 },
    { name: '蓝筹精选', value: 4.64 },
    { name: '鹏华弘利(固收+)', value: 7.86 },
    { name: '全球高端制造', value: 3.18 },
    { name: '新兴市场', value: 1.55 },
    { name: '中证1000(减半)', value: 0.36 },
    { name: '中证A50', value: 0 },
    { name: '沪深300增强', value: -0.84 },
    { name: '中证500增强', value: -1.34 },
    { name: '科鑫量化(清仓)', value: -2.28 },
    { name: '集成电路(止损)', value: -15.10 }
  ];
  chartReturns.setOption({
    animation: false,
    tooltip: {
      trigger: 'axis', appendToBody: true,
      formatter: function(params) {
        var p = params[0];
        var color = p.value >= 0 ? up : down;
        return p.name + '<br/>收益率: <b style="color:' + color + '">' + p.value.toFixed(2) + '%</b>';
      }
    },
    grid: { left: '3%', right: '6%', bottom: '3%', top: '5%', containLabel: true },
    xAxis: {
      type: 'value',
      axisLabel: { color: muted, fontSize: 10, formatter: '{value}%' },
      splitLine: { lineStyle: { color: rule } },
      axisLine: { lineStyle: { color: rule } }
    },
    yAxis: {
      type: 'category',
      data: returnsData.map(function(d) { return d.name; }).reverse(),
      axisLabel: { color: ink, fontSize: 11 },
      axisLine: { lineStyle: { color: rule } }
    },
    series: [{
      type: 'bar',
      data: returnsData.map(function(d) {
        return {
          value: d.value,
          itemStyle: { color: d.value >= 0 ? up : down, borderRadius: [0, 3, 3, 0] }
        };
      }).reverse(),
      barWidth: '60%',
      markLine: {
        data: [{ xAxis: 0 }],
        lineStyle: { color: rule, type: 'solid' },
        label: { show: false }
      }
    }]
  });
  window.addEventListener('resize', function() { chartReturns.resize(); });

  // ===== 板块强弱热力图 =====
  var chartHeatmap = echarts.init(document.getElementById('chart-sector-heat'), null, { renderer: 'svg' });
  var sectors = ['半导体/芯片', '集成电路', '科创创业', '中概互联', '高端制造', '纳斯达克', '沪深300', '中证500', '中证1000', '亚洲市场', '大盘蓝筹', '新兴市场'];
  var periods = ['5日', '10日', '20日', '60日', '120日'];
  // 动量分矩阵: 正=强势, 负=弱势
  var heatData = [];
  var heatMatrix = [
    [ -5.2,  8.4, 15.6, 28.3, 35.1],  // 半导体
    [-12.1, -3.5,  6.2, 10.8,  8.4],  // 集成电路
    [ -3.8,  5.1, 12.3, 22.5, 18.7],  // 科创创业
    [  1.2,  3.1,  4.5,  6.8,  3.2],  // 中概互联
    [ -1.5,  2.3,  5.8, 11.2, 13.5],  // 高端制造
    [  0.8, -0.5,  2.1,  5.6,  8.3],  // 纳斯达克
    [ -2.1, -1.3,  0.8,  3.2,  2.7],  // 沪深300
    [ -3.5, -2.8, -1.2,  1.5,  0.8],  // 中证500
    [ -4.2, -3.1, -2.8, -1.5, -3.2],  // 中证1000
    [  0.5,  1.8,  3.2,  7.5,  9.1],  // 亚洲市场
    [ -0.8,  0.3,  1.5,  4.2,  5.8],  // 大盘蓝筹
    [ -1.2, -0.8,  0.5,  2.1,  1.5]   // 新兴市场
  ];
  for (var i = 0; i < sectors.length; i++) {
    for (var j = 0; j < periods.length; j++) {
      heatData.push([j, i, heatMatrix[i][j]]);
    }
  }
  chartHeatmap.setOption({
    animation: false,
    tooltip: {
      appendToBody: true,
      formatter: function(p) {
        return sectors[p.value[1]] + ' · ' + periods[p.value[0]] + '<br/>动量: <b>' + p.value[2].toFixed(1) + '%</b>';
      }
    },
    grid: { left: '15%', right: '8%', bottom: '8%', top: '5%' },
    xAxis: {
      type: 'category',
      data: periods,
      axisLabel: { color: muted, fontSize: 11 },
      splitArea: { show: false },
      axisLine: { lineStyle: { color: rule } }
    },
    yAxis: {
      type: 'category',
      data: sectors,
      axisLabel: { color: ink, fontSize: 11 },
      splitArea: { show: false },
      axisLine: { lineStyle: { color: rule } }
    },
    visualMap: {
      min: -15, max: 35,
      calculable: true,
      orient: 'horizontal',
      left: 'center', bottom: 0,
      textStyle: { color: muted, fontSize: 10 },
      inRange: { color: [down, bg2, accent2, up] }
    },
    series: [{
      type: 'heatmap',
      data: heatData,
      label: {
        show: true,
        color: ink,
        fontSize: 10,
        formatter: function(p) { return p.value[2].toFixed(1); }
      },
      emphasis: { itemStyle: { shadowBlur: 8, shadowColor: 'rgba(0,0,0,0.3)' } }
    }]
  });
  window.addEventListener('resize', function() { chartHeatmap.resize(); });
})();
