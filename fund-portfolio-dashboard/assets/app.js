// ===== 主应用逻辑 =====
// 数据驱动渲染 + 截图上传 + AI Vision 解析 + localStorage 持久化

var App = (function() {
  var data = null;
  var charts = {};

  // ===== 初始化 =====
  function init() {
    loadData();
    renderAll();
    bindEvents();
  }

  // ===== 数据加载 =====
  function loadData() {
    var saved = localStorage.getItem('fund_portfolio_data');
    if (saved) {
      try {
        data = JSON.parse(saved);
        return;
      } catch(e) {
        console.warn('localStorage 数据解析失败，使用默认数据');
      }
    }
    data = JSON.parse(JSON.stringify(DEFAULT_DATA));
  }

  // ===== 保存数据 =====
  function saveData() {
    localStorage.setItem('fund_portfolio_data', JSON.stringify(data));
  }

  // ===== 重置为默认数据 =====
  function resetData() {
    data = JSON.parse(JSON.stringify(DEFAULT_DATA));
    saveData();
    renderAll();
  }

  // ===== CSS 变量读取 =====
  function getColors() {
    var s = getComputedStyle(document.documentElement);
    return {
      accent: s.getPropertyValue('--accent').trim(),
      accent2: s.getPropertyValue('--accent2').trim(),
      ink: s.getPropertyValue('--ink').trim(),
      muted: s.getPropertyValue('--muted').trim(),
      rule: s.getPropertyValue('--rule').trim(),
      bg2: s.getPropertyValue('--bg2').trim(),
      up: s.getPropertyValue('--up').trim(),
      down: s.getPropertyValue('--down').trim(),
      warn: s.getPropertyValue('--warn').trim()
    };
  }

  // ===== 动量状态映射 =====
  var MOMENTUM_LABELS = {
    'strong': { text: '🟢 强势动量', tag: 'tag-green' },
    'strong-volatile': { text: '🟢 强⚠波动', tag: 'tag-green' },
    'recover': { text: '🟡 企稳反弹', tag: 'tag-green' },
    'weak': { text: '🟡 温和/走弱', tag: 'tag-yellow' },
    'neutral': { text: '⚪ 中性/新仓', tag: 'tag-gray' },
    'down': { text: '🔴 动量恶化', tag: 'tag-red' }
  };

  var CATEGORY_TAGS = {
    'A股宽基': 'tag-blue',
    '行业主题': 'tag-purple',
    'QDII海外': 'tag-green',
    '固收+': 'tag-blue'
  };

  // ===== 计算汇总数据 =====
  function getSummary() {
    var equity = 0, fixedIncome = 0, total = 0;
    var categoryTotals = {};
    data.holdings.forEach(function(h) {
      total += h.amount;
      categoryTotals[h.category] = (categoryTotals[h.category] || 0) + h.amount;
      if (h.category === '固收+') {
        fixedIncome += h.amount;
      } else {
        equity += h.amount;
      }
    });
    return {
      total: total,
      equity: equity,
      fixedIncome: fixedIncome,
      categoryTotals: categoryTotals,
      fundCount: data.holdings.length
    };
  }

  // ===== 渲染统计卡片 =====
  function renderStats() {
    var s = getSummary();
    var cashRes = data.holdings.filter(function(h) { return h.action === 'reduced'; });
    var statGrid = document.getElementById('stat-grid');
    statGrid.innerHTML =
      '<div class="stat-card"><div class="label">总资产</div><div class="value">¥' + formatWan(s.total) + '万</div><div class="sub">' + s.fundCount + ' 只基金</div></div>' +
      '<div class="stat-card"><div class="label">权益仓位</div><div class="value" style="color:var(--accent)">¥' + formatWan(s.equity) + '万</div><div class="sub">' + (s.equity/s.total*100).toFixed(0) + '%</div></div>' +
      '<div class="stat-card"><div class="label">固收+底仓</div><div class="value" style="color:var(--up)">¥' + formatWan(s.fixedIncome) + '万</div><div class="sub">' + (s.fixedIncome/s.total*100).toFixed(0) + '%</div></div>' +
      '<div class="stat-card"><div class="label">更新时间</div><div class="value" style="font-size:1.1rem">' + (data.lastUpdated || '—') + '</div><div class="sub">上传截图可更新</div></div>';
  }

  // ===== 渲染持仓表格 =====
  function renderTable() {
    var tbody = document.getElementById('holdings-tbody');
    var html = '';
    data.holdings.forEach(function(h, i) {
      var momLabel = MOMENTUM_LABELS[h.momentumStatus] || MOMENTUM_LABELS['neutral'];
      var catTag = CATEGORY_TAGS[h.category] || 'tag-gray';
      var retClass = h.returnRate > 0 ? 'num-up' : (h.returnRate < 0 ? 'num-down' : 'num-muted');
      var retSign = h.returnRate > 0 ? '+' : '';
      var noteSpan = h.note ? ' <span class="num-muted" style="font-size:0.72rem">' + h.note + '</span>' : '';

      html += '<tr>' +
        '<td>' + (i+1) + '</td>' +
        '<td>' + h.name + '</td>' +
        '<td><span class="tag ' + catTag + '">' + h.category + '</span></td>' +
        '<td>¥' + h.amount.toLocaleString() + noteSpan + '</td>' +
        '<td class="' + retClass + '">' + retSign + h.returnRate.toFixed(2) + '%</td>' +
        '<td><span class="tag ' + momLabel.tag + '">' + momLabel.text + '</span></td>' +
        '<td style="font-size:0.82rem;color:var(--muted)">' + h.benchmark + '</td>' +
        '<td>' + h.advice + '</td>' +
        '<td><span class="tag tag-' + h.actionColor + '">' + h.actionLabel + '</span></td>' +
        '</tr>';
    });
    tbody.innerHTML = html;
  }

  // ===== 渲染新闻 =====
  function renderNews() {
    var container = document.getElementById('news-list');
    var html = '';
    data.news.forEach(function(n) {
      var impactColor = n.impactColor === 'up' ? 'var(--up)' : (n.impactColor === 'down' ? 'var(--down)' : 'var(--warn)');
      html += '<div class="news-item">' +
        '<div class="news-icon ' + n.type + '">' + n.icon + '</div>' +
        '<div class="news-content">' +
        '<h4>' + n.title + '</h4>' +
        '<p>' + n.desc + '</p>' +
        '<div class="news-impact" style="color:' + impactColor + '">→ ' + n.impact + '</div>' +
        '</div></div>';
    });
    container.innerHTML = html;
  }

  // ===== 渲染市场环境 =====
  function renderMarketContext() {
    var mc = data.marketContext;
    document.getElementById('market-subtitle').textContent =
      '数据截止：' + data.lastUpdated + ' | 上证 ' + mc.shIndex + ' | 两市成交 ' + mc.volume;
    var badge = document.getElementById('regime-badge');
    badge.textContent = '⚠ Regime: ' + mc.regime;
    badge.className = 'badge badge-' + (mc.regimeType || 'risk');
  }

  // ===== 渲染所有图表 =====
  function renderCharts() {
    var c = getColors();
    var s = getSummary();

    // 销毁旧图表
    Object.keys(charts).forEach(function(k) {
      if (charts[k]) { charts[k].dispose(); }
    });
    charts = {};

    // --- 资产配置饼图 ---
    var allocData = [];
    Object.keys(s.categoryTotals).forEach(function(cat) {
      var color = cat === '固收+' ? '#5B8DEF' :
                  cat === 'A股宽基' ? c.accent :
                  cat === '行业主题' ? c.accent2 :
                  cat === 'QDII海外' ? '#E8A838' : '#6B7B8E';
      allocData.push({ value: s.categoryTotals[cat], name: cat, itemStyle: { color: color } });
    });

    charts.alloc = echarts.init(document.getElementById('chart-allocation'), null, { renderer: 'svg' });
    charts.alloc.setOption({
      animation: false,
      tooltip: { trigger: 'item', appendToBody: true, formatter: '{b}<br/>¥{c} ({d}%)' },
      legend: { orient: 'vertical', right: '5%', top: 'center', textStyle: { color: c.muted, fontSize: 12 }, itemWidth: 12, itemHeight: 12 },
      series: [{ type: 'pie', radius: ['40%', '70%'], center: ['38%', '50%'], label: { show: false }, labelLine: { show: false }, data: allocData }]
    });

    // --- 风格暴露雷达图 ---
    var radarValues = computeRadar(s);
    charts.radar = echarts.init(document.getElementById('chart-radar'), null, { renderer: 'svg' });
    charts.radar.setOption({
      animation: false,
      tooltip: { appendToBody: true },
      radar: {
        indicator: [
          { name: '大盘价值', max: 50 }, { name: '大盘成长', max: 50 },
          { name: '小盘成长', max: 50 }, { name: '科技/半导体', max: 50 },
          { name: '海外配置', max: 50 }, { name: '固收防御', max: 50 }
        ],
        axisName: { color: c.ink, fontSize: 12 },
        splitLine: { lineStyle: { color: c.rule } },
        splitArea: { areaStyle: { color: ['transparent', c.bg2] } },
        axisLine: { lineStyle: { color: c.rule } }
      },
      series: [{
        type: 'radar',
        data: [
          { value: radarValues, name: '当前暴露', areaStyle: { color: c.accent + '33' }, lineStyle: { color: c.accent, width: 2 }, itemStyle: { color: c.accent } },
          { value: [22, 10, 10, 8, 18, 32], name: '目标暴露', areaStyle: { color: c.accent2 + '22' }, lineStyle: { color: c.accent2, width: 2, type: 'dashed' }, itemStyle: { color: c.accent2 } }
        ]
      }],
      legend: { data: ['当前暴露', '目标暴露'], bottom: 0, textStyle: { color: c.muted, fontSize: 11 } }
    });

    // --- 动量评分柱状图 ---
    var names = data.holdings.map(function(h) { return h.name.replace(/易方达|长城|浦银安盛|长信|财通|富国|德邦|博时|天弘|国富|建信|前海开源|鹏华/g, '').substring(0, 6); });
    var scores = data.holdings.map(function(h) { return h.momentum; });
    var barColors = scores.map(function(v) {
      if (v >= 2.5) return c.up;
      if (v >= 0.5) return c.accent;
      if (v >= -1) return c.warn;
      return c.down;
    });
    charts.momentum = echarts.init(document.getElementById('chart-momentum'), null, { renderer: 'svg' });
    charts.momentum.setOption({
      animation: false,
      tooltip: { trigger: 'axis', appendToBody: true, formatter: function(p) {
        var v = p[0].value;
        var status = v >= 2.5 ? '🟢 强势动量' : v >= 0.5 ? '🟡 温和向上' : v >= -1 ? '🟠 动量走弱' : '🔴 动量恶化';
        return p[0].name + '<br/>动量分: ' + v.toFixed(1) + '<br/>' + status;
      }},
      grid: { left: '3%', right: '4%', bottom: '20%', top: '5%', containLabel: true },
      xAxis: { type: 'category', data: names, axisLabel: { color: c.muted, fontSize: 9, interval: 0, rotate: 30 }, axisLine: { lineStyle: { color: c.rule } } },
      yAxis: { type: 'value', name: '动量分', nameTextStyle: { color: c.muted, fontSize: 10 }, axisLabel: { color: c.muted, fontSize: 10 }, splitLine: { lineStyle: { color: c.rule } }, axisLine: { lineStyle: { color: c.rule } } },
      series: [{ type: 'bar', data: scores.map(function(v, i) { return { value: v, itemStyle: { color: barColors[i], borderRadius: [3, 3, 0, 0] } }; }), barWidth: '55%',
        markLine: { data: [{ yAxis: 0 }], lineStyle: { color: c.rule }, label: { show: false } } }]
    });

    // --- 收益分布柱状图 ---
    var sorted = data.holdings.slice().sort(function(a, b) { return a.returnRate - b.returnRate; });
    charts.returns = echarts.init(document.getElementById('chart-returns'), null, { renderer: 'svg' });
    charts.returns.setOption({
      animation: false,
      tooltip: { trigger: 'axis', appendToBody: true, formatter: function(p) {
        var v = p[0].value; var color = v >= 0 ? c.up : c.down;
        return p[0].name + '<br/>收益率: <b style="color:' + color + '">' + v.toFixed(2) + '%</b>';
      }},
      grid: { left: '3%', right: '6%', bottom: '3%', top: '5%', containLabel: true },
      xAxis: { type: 'value', axisLabel: { color: c.muted, fontSize: 10, formatter: '{value}%' }, splitLine: { lineStyle: { color: c.rule } }, axisLine: { lineStyle: { color: c.rule } } },
      yAxis: { type: 'category', data: sorted.map(function(h) { return h.name.substring(0, 12); }), axisLabel: { color: c.ink, fontSize: 11 }, axisLine: { lineStyle: { color: c.rule } } },
      series: [{ type: 'bar', data: sorted.map(function(h) { return { value: h.returnRate, itemStyle: { color: h.returnRate >= 0 ? c.up : c.down, borderRadius: [0, 3, 3, 0] } }; }), barWidth: '60%',
        markLine: { data: [{ xAxis: 0 }], lineStyle: { color: c.rule }, label: { show: false } } }]
    });

    // --- 板块热力图 ---
    var sh = data.sectorHeat;
    var heatData = [];
    for (var i = 0; i < sh.sectors.length; i++) {
      for (var j = 0; j < sh.periods.length; j++) {
        heatData.push([j, i, sh.matrix[i][j]]);
      }
    }
    charts.heat = echarts.init(document.getElementById('chart-sector-heat'), null, { renderer: 'svg' });
    charts.heat.setOption({
      animation: false,
      tooltip: { appendToBody: true, formatter: function(p) { return sh.sectors[p.value[1]] + ' · ' + sh.periods[p.value[0]] + '<br/>动量: <b>' + p.value[2].toFixed(1) + '%</b>'; } },
      grid: { left: '15%', right: '8%', bottom: '12%', top: '5%' },
      xAxis: { type: 'category', data: sh.periods, axisLabel: { color: c.muted, fontSize: 11 }, splitArea: { show: false }, axisLine: { lineStyle: { color: c.rule } } },
      yAxis: { type: 'category', data: sh.sectors, axisLabel: { color: c.ink, fontSize: 11 }, splitArea: { show: false }, axisLine: { lineStyle: { color: c.rule } } },
      visualMap: { min: -15, max: 35, calculable: true, orient: 'horizontal', left: 'center', bottom: 0, textStyle: { color: c.muted, fontSize: 10 }, inRange: { color: [c.down, c.bg2, c.accent2, c.up] } },
      series: [{ type: 'heatmap', data: heatData, label: { show: true, color: c.ink, fontSize: 10, formatter: function(p) { return p.value[2].toFixed(1); } }, emphasis: { itemStyle: { shadowBlur: 8 } } }]
    });

    // resize listener
    window.addEventListener('resize', handleResize);
  }

  function handleResize() {
    Object.keys(charts).forEach(function(k) { if (charts[k]) charts[k].resize(); });
  }

  // ===== 计算雷达图数值 =====
  function computeRadar(s) {
    var ct = s.categoryTotals;
    var total = s.total;
    var holdings = data.holdings;
    var largeCapValue = 0, largeCapGrowth = 0, smallCap = 0, tech = 0, overseas = 0, fixedDef = 0;

    holdings.forEach(function(h) {
      var pct = h.amount / total * 100;
      if (h.category === '固收+') { fixedDef += pct; }
      else if (h.category === 'QDII海外') { overseas += pct; }
      else if (h.name.indexOf('沪深300') >= 0 || h.name.indexOf('蓝筹') >= 0 || h.name.indexOf('A50') >= 0) { largeCapValue += pct; }
      else if (h.name.indexOf('1000') >= 0 || h.name.indexOf('500') >= 0) { smallCap += pct; }
      else if (h.name.indexOf('半导体') >= 0 || h.name.indexOf('集成电路') >= 0 || h.name.indexOf('科创') >= 0 || h.name.indexOf('高端制造') >= 0 || h.name.indexOf('互联科技') >= 0) { tech += pct; }
      else { largeCapGrowth += pct; }
    });
    return [Math.round(largeCapValue), Math.round(largeCapGrowth), Math.round(smallCap), Math.round(tech), Math.round(overseas), Math.round(fixedDef)];
  }

  // ===== 格式化万元 =====
  function formatWan(num) {
    return (num / 10000).toFixed(1);
  }

  // ===== 渲染全部 =====
  function renderAll() {
    renderMarketContext();
    renderStats();
    renderTable();
    renderNews();
    renderCharts();
  }

  // ===== 截图上传 + AI 解析 =====

  function bindEvents() {
    // 文件选择
    document.getElementById('file-input').addEventListener('change', handleFileSelect);

    // 拖拽
    var dropZone = document.getElementById('upload-zone');
    dropZone.addEventListener('dragover', function(e) { e.preventDefault(); dropZone.classList.add('drag-over'); });
    dropZone.addEventListener('dragleave', function() { dropZone.classList.remove('drag-over'); });
    dropZone.addEventListener('drop', function(e) {
      e.preventDefault();
      dropZone.classList.remove('drag-over');
      handleFiles(e.dataTransfer.files);
    });

    // 点击上传区也触发选择
    dropZone.addEventListener('click', function() {
      document.getElementById('file-input').click();
    });

    // API 配置保存
    document.getElementById('api-save-btn').addEventListener('click', saveApiConfig);

    // 重置数据
    document.getElementById('reset-btn').addEventListener('click', function() {
      if (confirm('确定要重置为默认数据吗？当前数据将丢失。')) {
        resetData();
      }
    });

    // 手动编辑
    document.getElementById('edit-btn').addEventListener('click', toggleEditMode);

    // 加载已保存的 API 配置
    loadApiConfig();
  }

  function handleFileSelect(e) {
    handleFiles(e.target.files);
  }

  // 待解析的图片队列
  var pendingImages = [];
  var currentImageIndex = 0;

  function handleFiles(files) {
    var images = [];
    for (var i = 0; i < files.length; i++) {
      if (files[i].type.startsWith('image/')) {
        images.push(files[i]);
      }
    }
    if (images.length === 0) {
      alert('请上传图片文件（截图）');
      return;
    }

    // 检查 API 配置
    var config = getApiConfig();
    if (!config.apiKey) {
      alert('请先配置 AI API Key（在页面上方的设置区域输入）');
      return;
    }

    pendingImages = images;
    currentImageIndex = 0;
    document.getElementById('upload-zone').innerHTML = '<div class="upload-processing"><div class="spinner"></div><p>正在解析截图... (1/' + images.length + ')</p></div>';

    processNextImage();
  }

  function processNextImage() {
    if (currentImageIndex >= pendingImages.length) {
      // 所有图片处理完毕
      finishProcessing();
      return;
    }

    var file = pendingImages[currentImageIndex];
    var reader = new FileReader();
    reader.onload = function(e) {
      var base64 = e.target.result;
      callAIVision(base64, file.type);
    };
    reader.readAsDataURL(file);
  }

  function callAIVision(base64Image, mimeType) {
    var config = getApiConfig();
    var pureBase64 = base64Image.split(',')[1];

    // 构建请求
    var requestBody = {
      model: config.model,
      messages: [
        {
          role: "system",
          content: "你是一个基金持仓分析助手。用户会上传基金APP的持仓截图，你需要识别出其中的基金信息，并以JSON数组格式返回。每只基金包含：name(基金名称)、amount(持有金额,数字)、returnRate(持有收益率百分比,数字)。注意：收益率-0.84%就是-0.84。只返回JSON数组，不要其他文字。"
        },
        {
          role: "user",
          content: [
            { type: "text", text: "请识别这张基金持仓截图中的所有基金信息，以JSON数组格式返回。" },
            { type: "image_url", image_url: { url: base64Image } }
          ]
        }
      ],
      temperature: 0.1,
      max_tokens: 2000
    };

    fetch(config.endpoint, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer ' + config.apiKey
      },
      body: JSON.stringify(requestBody)
    })
    .then(function(res) {
      if (!res.ok) throw new Error('API 返回错误: ' + res.status);
      return res.json();
    })
    .then(function(result) {
      var content = result.choices[0].message.content;
      var parsed = parseAIResponse(content);
      if (parsed && parsed.length > 0) {
        applyParsedData(parsed);
      }
      currentImageIndex++;
      if (currentImageIndex < pendingImages.length) {
        // 更新进度
        document.querySelector('.upload-processing p').textContent =
          '正在解析截图... (' + (currentImageIndex + 1) + '/' + pendingImages.length + ')';
        processNextImage();
      } else {
        finishProcessing();
      }
    })
    .catch(function(err) {
      console.error('AI 解析失败:', err);
      document.getElementById('upload-zone').innerHTML =
        '<div class="upload-error">❌ 解析失败: ' + err.message + '<br><span style="font-size:0.82rem">请检查 API 配置是否正确</span></div>';
      restoreUploadZone();
    });
  }

  // 解析 AI 返回的 JSON
  function parseAIResponse(content) {
    // 尝试从回复中提取 JSON 数组
    var jsonMatch = content.match(/\[[\s\S]*\]/);
    if (jsonMatch) {
      try {
        return JSON.parse(jsonMatch[0]);
      } catch(e) {
        console.warn('JSON 解析失败', e);
      }
    }
    // 尝试直接解析
    try {
      return JSON.parse(content);
    } catch(e) {
      console.warn('无法解析 AI 返回的内容:', content);
      return null;
    }
  }

  // 将解析的数据合并到现有持仓中
  function applyParsedData(parsedFunds) {
    parsedFunds.forEach(function(fund) {
      // 尝试匹配现有基金（名称模糊匹配）
      var matched = null;
      for (var i = 0; i < data.holdings.length; i++) {
        var h = data.holdings[i];
        // 去掉空格后比较
        var hName = h.name.replace(/\s+/g, '');
        var fName = (fund.name || '').replace(/\s+/g, '');
        if (hName.indexOf(fName) >= 0 || fName.indexOf(hName) >= 0) {
          matched = h;
          break;
        }
        // 部分关键词匹配
        var keywords = ['沪深300', '中证1000', '中证500', 'A50', '科创', '集成电路', '互联科技', '半导体', '高端制造', '科鑫', '蓝筹', '纳斯达克', '纳指', '全球成长', '全球高端', '亚洲', '新兴市场', '鹏华弘利'];
        for (var k = 0; k < keywords.length; k++) {
          if (hName.indexOf(keywords[k]) >= 0 && fName.indexOf(keywords[k]) >= 0) {
            matched = h;
            break;
          }
        }
        if (matched) break;
      }

      if (matched) {
        // 更新现有基金
        if (fund.amount !== undefined) matched.amount = Math.round(fund.amount);
        if (fund.returnRate !== undefined) matched.returnRate = fund.returnRate;
      } else {
        // 新基金，添加到列表
        data.holdings.push({
          id: data.holdings.length + 1,
          name: fund.name || '未知基金',
          category: '行业主题',
          categoryColor: 'purple',
          amount: Math.round(fund.amount) || 0,
          returnRate: fund.returnRate || 0,
          momentum: 0,
          momentumStatus: 'neutral',
          benchmark: '',
          advice: '待分析',
          action: 'hold',
          actionLabel: '新增',
          actionColor: 'blue',
          note: '截图识别'
        });
      }
    });

    // 更新日期
    data.lastUpdated = new Date().toISOString().slice(0, 10);
    saveData();
    renderAll();
  }

  function finishProcessing() {
    document.getElementById('upload-zone').innerHTML =
      '<div class="upload-success">✅ 持仓已更新！共处理 ' + pendingImages.length + ' 张截图</div>';
    setTimeout(restoreUploadZone, 3000);
  }

  function restoreUploadZone() {
    document.getElementById('upload-zone').innerHTML =
      '<div class="upload-icon">📸</div>' +
      '<p>点击或拖拽基金持仓截图到此处</p>' +
      '<p class="upload-hint">支持多张截图批量上传 · AI 自动识别基金名称、金额、收益率</p>';
  }

  // ===== API 配置管理 =====
  function getApiConfig() {
    return {
      endpoint: localStorage.getItem('ai_endpoint') || 'https://api.deepseek.com/v1/chat/completions',
      apiKey: localStorage.getItem('ai_apikey') || '',
      model: localStorage.getItem('ai_model') || 'deepseek-chat'
    };
  }

  function saveApiConfig() {
    var endpoint = document.getElementById('api-endpoint').value.trim();
    var apiKey = document.getElementById('api-key').value.trim();
    var model = document.getElementById('api-model').value.trim();

    if (!apiKey) { alert('请输入 API Key'); return; }

    localStorage.setItem('ai_endpoint', endpoint || 'https://api.deepseek.com/v1/chat/completions');
    localStorage.setItem('ai_apikey', apiKey);
    localStorage.setItem('ai_model', model || 'deepseek-chat');
    alert('✅ API 配置已保存');
  }

  function loadApiConfig() {
    var config = getApiConfig();
    document.getElementById('api-endpoint').value = config.endpoint;
    document.getElementById('api-key').value = config.apiKey;
    document.getElementById('api-model').value = config.model;
  }

  // ===== 手动编辑模式 =====
  var editMode = false;

  function toggleEditMode() {
    editMode = !editMode;
    var tbody = document.getElementById('holdings-tbody');
    if (editMode) {
      document.getElementById('edit-btn').textContent = '💾 保存修改';
      renderEditableTable();
    } else {
      document.getElementById('edit-btn').textContent = '✏️ 手动编辑';
      saveData();
      renderAll();
    }
  }

  function renderEditableTable() {
    var tbody = document.getElementById('holdings-tbody');
    var html = '';
    data.holdings.forEach(function(h, i) {
      html += '<tr>' +
        '<td>' + (i+1) + '</td>' +
        '<td><input type="text" value="' + h.name + '" onchange="App.updateField(' + i + ',\'name\',this.value)" style="width:100%;background:var(--bg3);border:1px solid var(--rule);color:var(--ink);border-radius:4px;padding:2px 6px;font-size:0.82rem"></td>' +
        '<td><select onchange="App.updateField(' + i + ',\'category\',this.value)" style="background:var(--bg3);border:1px solid var(--rule);color:var(--ink);border-radius:4px;padding:2px;font-size:0.82rem">' +
          ['A股宽基','行业主题','QDII海外','固收+'].map(function(c) {
            return '<option ' + (c===h.category?'selected':'') + '>' + c + '</option>';
          }).join('') +
        '</select></td>' +
        '<td><input type="number" value="' + h.amount + '" onchange="App.updateField(' + i + ',\'amount\',parseFloat(this.value))" style="width:80px;background:var(--bg3);border:1px solid var(--rule);color:var(--ink);border-radius:4px;padding:2px 6px;font-size:0.82rem"></td>' +
        '<td><input type="number" step="0.01" value="' + h.returnRate + '" onchange="App.updateField(' + i + ',\'returnRate\',parseFloat(this.value))" style="width:60px;background:var(--bg3);border:1px solid var(--rule);color:var(--ink);border-radius:4px;padding:2px 6px;font-size:0.82rem"></td>' +
        '<td colspan="4"><button onclick="App.deleteHolding(' + i + ')" style="background:rgba(248,81,73,0.15);color:var(--down);border:1px solid rgba(248,81,73,0.3);border-radius:4px;padding:2px 8px;cursor:pointer;font-size:0.78rem">🗑 删除</button></td>' +
        '</tr>';
    });
    tbody.innerHTML = html;
  }

  function updateField(index, field, value) {
    if (data.holdings[index]) {
      data.holdings[index][field] = value;
    }
  }

  function deleteHolding(index) {
    data.holdings.splice(index, 1);
    renderEditableTable();
  }

  // ===== 公开 API =====
  return {
    init: init,
    updateField: updateField,
    deleteHolding: deleteHolding
  };
})();

// 启动
document.addEventListener('DOMContentLoaded', function() {
  App.init();
});
