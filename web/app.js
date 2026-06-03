const state = {
  mode: "live",
  timer: null,
  loadingRankings: false,
  refreshMs: 60_000,
  playing: false,
  playTimer: null,
  livePinned: false,
  liveCursor: null,
  lastLiveAsOf: null,
  lastRows: [],
  sideCounts: null,
  sideCountDeltas: { long: 0, neutral: 0, short: 0 },
  fadesFirst: false,
};

const $ = (id) => document.getElementById(id);
const pinnedMarketSymbols = ["SPY", "QQQ"];
const sideCountGroups = [
  { key: "long", label: "Long", side: "Long bounce" },
  { key: "neutral", label: "Neutral", side: "Neutral" },
  { key: "short", label: "Short", side: "Short fade" },
];

function fmtDateTime(value) {
  if (!value) return "-";
  return new Date(value).toLocaleString([], {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function fmtClock(value) {
  if (!value) return "-";
  return new Date(value).toLocaleString([], {
    weekday: "short",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    timeZoneName: "short",
  });
}

function fmtMoney(value) {
  if (!Number.isFinite(value)) return "-";
  if (value >= 1_000_000_000) return `$${(value / 1_000_000_000).toFixed(1)}B`;
  if (value >= 1_000_000) return `$${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `$${(value / 1_000).toFixed(1)}K`;
  return `$${value.toFixed(0)}`;
}

function fmtVolume(value) {
  if (!Number.isFinite(value)) return "-";
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)}B`;
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return value.toFixed(0);
}

function signed(value, suffix = "") {
  if (!Number.isFinite(value)) return "-";
  const prefix = value > 0 ? "+" : "";
  return `${prefix}${value.toFixed(2)}${suffix}`;
}

function priorCloseChangePct(row) {
  const price = Number(row.price);
  const priorClose = Number(row.mini_chart?.prior_close);
  if (!Number.isFinite(price) || !Number.isFinite(priorClose) || priorClose <= 0) return NaN;
  return ((price - priorClose) / priorClose) * 100;
}

function renderPrice(row) {
  const price = Number(row.price || 0).toFixed(2);
  const changePct = priorCloseChangePct(row);
  const direction = changePct > 0 ? "up" : changePct < 0 ? "down" : "flat";
  const label = Number.isFinite(changePct) ? signed(changePct, "%") : "-";
  const title = Number.isFinite(changePct) ? "Change from prior close" : "Prior close unavailable";
  return `
    <span class="price-cell">
      <span class="last-price">${price}</span>
      <span class="price-change ${direction}" title="${title}">${label}</span>
    </span>`;
}

async function loadConfig() {
  const res = await fetch("/api/config", { cache: "no-store" });
  const data = await res.json();
  renderFormula(data.formula);
  $("symbolCount").textContent = data.symbols.length;
  const seconds = Number(data.config?.live?.refresh_seconds);
  if (Number.isFinite(seconds) && seconds > 0) {
    state.refreshMs = seconds * 1000;
  }

  const now = new Date();
  $("histDate").value = now.toISOString().slice(0, 10);
  $("histTime").value = "20:00";
}

function renderFormula(formula) {
  $("formulaSummary").textContent = formula.summary;
  $("formulaGrid").innerHTML = Object.entries(formula.components)
    .map(([key, value]) => `<div class="formula-item"><strong>${key}</strong><span>${value}</span></div>`)
    .join("");
}

async function loadRankings() {
  if (state.loadingRankings) return;
  state.loadingRankings = true;
  const pinnedLive = state.mode === "live" && state.livePinned;
  const url = state.mode === "historical" ? historicalUrl() : pinnedLive ? liveCursorUrl() : "/api/rankings";
  try {
    const res = await fetch(url, { cache: "no-store" });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || "request failed");
    if (pinnedLive) {
      data.mode = "live paused";
    }
    renderSnapshot(data);
  } finally {
    state.loadingRankings = false;
    scheduleLiveRefresh();
  }
}

function historicalUrl() {
  const params = new URLSearchParams({
    mode: "historical",
    date: $("histDate").value,
    time: $("histTime").value,
  });
  return `/api/rankings?${params.toString()}`;
}

function liveCursorUrl() {
  ensureLiveCursor();
  const parts = dateTimeParts(state.liveCursor);
  const params = new URLSearchParams({
    mode: "historical",
    date: parts.date,
    time: parts.time,
  });
  return `/api/rankings?${params.toString()}`;
}

function renderSnapshot(data) {
  $("status").textContent = `${data.mode} - ${data.status}`;
  $("symbolCount").textContent = data.symbols;
  $("asOf").textContent = fmtDateTime(data.as_of);
  $("updatedAt").textContent = fmtDateTime(data.updated_at);
  $("dataClock").textContent = fmtClock(data.as_of);
  $("dataClockMode").textContent = dataClockModeLabel(data.mode);
  if (state.mode === "live" && !state.livePinned && data.as_of) {
    state.lastLiveAsOf = new Date(data.as_of);
    state.liveCursor = new Date(data.as_of);
  }
  updateLiveControls();
  if (data.formula) renderFormula(data.formula);
  state.lastRows = data.rankings || [];
  updateSideCounts(state.lastRows.filter((row) => !isPinnedMarket(row)));
  renderRows(state.lastRows);
}

function dataClockModeLabel(apiMode) {
  if (state.mode === "historical") return "Historical replay minute";
  if (state.livePinned) return "Live paused minute";
  if (apiMode === "live paused") return "Live paused minute";
  return "Latest live minute";
}

function renderRows(rows) {
  const pinnedRows = pinnedMarketSymbols
    .map((symbol) => rows.find((row) => row.symbol === symbol))
    .filter(Boolean)
    .map((row) => ({ ...row, display_rank: row.rank }));
  const gridRows = rows.filter((row) => !isPinnedMarket(row));

  renderSideCounts();
  renderMarketPins(pinnedRows);
  renderGroupedRows(gridRows);
}

function updateSideCounts(rows) {
  const next = countSides(rows);
  if (state.sideCounts) {
    state.sideCountDeltas = Object.fromEntries(
      sideCountGroups.map((group) => [group.key, next[group.key] - state.sideCounts[group.key]])
    );
  } else {
    state.sideCountDeltas = { long: 0, neutral: 0, short: 0 };
  }
  state.sideCounts = next;
}

function countSides(rows) {
  return Object.fromEntries(
    sideCountGroups.map((group) => [
      group.key,
      rows.filter((row) => row.side === group.side).length,
    ])
  );
}

function renderSideCounts() {
  const counts = state.sideCounts || { long: 0, neutral: 0, short: 0 };
  $("sideCounts").innerHTML = sideCountGroups.map((group) => {
    const count = counts[group.key] || 0;
    const delta = state.sideCountDeltas[group.key] || 0;
    const deltaClass = delta > 0 ? "delta-up" : delta < 0 ? "delta-down" : "delta-flat";
    const deltaLabel = delta > 0 ? `+${delta}` : String(delta);
    return `
      <div class="side-count side-count-${group.key}">
        <span>${group.label}</span>
        <strong>${count}</strong>
        <em class="${deltaClass}">${deltaLabel}</em>
      </div>`;
  }).join("");
}

function renderMarketPins(rows) {
  $("marketPins").innerHTML = rows.map((row) => renderCandidateCard(row, "market-pin")).join("");
}

function renderGroupedRows(rows) {
  const orderedGroups = state.fadesFirst
    ? [
      { title: "Short", side: "Short fade" },
      { title: "Neutral", side: "Neutral" },
      { title: "Long", side: "Long bounce" },
    ]
    : [
      { title: "Long", side: "Long bounce" },
      { title: "Neutral", side: "Neutral" },
      { title: "Short", side: "Short fade" },
    ];
  const ranked = rowsWithSideRanks(rows);

  $("rankings").innerHTML = orderedGroups.map((group) => {
    const groupRows = ranked
      .filter((row) => row.side === group.side)
      .sort(compareCandidateScore);
    const body = groupRows.length
      ? groupRows.map((row) => renderCandidateCard(row)).join("")
      : `<div class="empty-group">No ${group.title.toLowerCase()} candidates</div>`;
    return `
      <section class="candidate-section" aria-label="${group.title} candidates">
        <div class="candidate-section-header">
          <h2>${group.title}</h2>
          <span>${groupRows.length}</span>
        </div>
        <div class="candidate-grid">${body}</div>
      </section>`;
  }).join("");
}

function renderCandidateCard(r, extraClass = "") {
  const gradeClass = (r.grade || "").toLowerCase();
  const sideClass = candidateSideClass(r.side);
  const cardSideClass = candidateCardSideClass(r.side);
  return `
    <article class="candidate-card ${cardSideClass} ${extraClass}">
      <div class="candidate-top">
        <div class="candidate-stat rank-stat">
          <span>Rank</span>
          <strong>${r.display_rank || "-"}</strong>
        </div>
        <div class="candidate-stat symbol-stat">
          <span>Symbol</span>
          <strong><a href="${r.chart_url || "#"}" target="_blank" rel="noreferrer">${r.symbol}</a></strong>
        </div>
        <div class="candidate-stat side-stat">
          <span>Side</span>
          <strong class="${sideClass}">${r.side}</strong>
        </div>
        <div class="candidate-stat score-stat">
          <span>Score</span>
          <strong><span class="grade ${gradeClass}">${r.grade}</span>${Number(r.score || 0).toFixed(1)}</strong>
        </div>
        <div class="candidate-stat price-stat">
          <span>Price</span>
          <strong>${renderPrice(r)}</strong>
        </div>
        <div class="candidate-stat volume-stat">
          <span>Dollar / Volume</span>
          <strong>${fmtMoney(r.dollar_volume || 0)} <em>${fmtVolume(r.session_volume || 0)}</em></strong>
        </div>
      </div>
      <div class="chart-wrap" tabindex="0" aria-label="${r.symbol} diagnostics">
        ${renderMiniChart(r)}
        ${renderMetricPopover(r)}
      </div>
      <div class="candidate-reason">
        <span>${r.reason || ""}</span>
        <em>${componentTitle(r.components)}</em>
      </div>
    </article>`;
}

function candidateSideClass(side) {
  if (side === "Long bounce") return "side-long";
  if (side === "Short fade") return "side-short";
  if (side === "Neutral") return "side-neutral";
  return "";
}

function candidateCardSideClass(side) {
  if (side === "Long bounce") return "card-long";
  if (side === "Short fade") return "card-short";
  if (side === "Neutral") return "card-neutral";
  return "";
}

function isPinnedMarket(row) {
  return pinnedMarketSymbols.includes(row.symbol);
}

function rowsWithSideRanks(rows) {
  const ranked = rows.map((row) => ({ ...row, display_rank: row.rank }));
  ["Long bounce", "Neutral", "Short fade"].forEach((side) => {
    ranked
      .filter((row) => row.side === side)
      .sort(compareCandidateScore)
      .forEach((row, index) => {
        row.display_rank = index + 1;
      });
  });
  return ranked;
}

function compareCandidateScore(a, b) {
  const scoreDiff = Number(b.score || 0) - Number(a.score || 0);
  if (scoreDiff !== 0) return scoreDiff;
  return String(a.symbol || "").localeCompare(String(b.symbol || ""));
}

function renderMetricPopover(r) {
  return `
    <div class="metric-popover" role="tooltip">
      <div><span>VWAP</span><strong>${Number(r.vwap || 0).toFixed(2)}</strong></div>
      <div><span>VWAP %</span><strong class="${r.move_from_vwap_pct < 0 ? "negative" : "positive"}">${signed(r.move_from_vwap_pct, "%")}</strong></div>
      <div><span>Day ATR</span><strong class="${r.day_move_atr < 0 ? "negative" : "positive"}">${signed(r.day_move_atr, "x")}</strong></div>
      <div><span>VWAP ATR</span><strong class="${r.vwap_stretch_atr < 0 ? "negative" : "positive"}">${signed(r.vwap_stretch_atr, "x")}</strong></div>
      <div><span>ATR %</span><strong>${Number(r.atr_percent || 0).toFixed(2)}%</strong></div>
      <div><span>30m %</span><strong class="${r.return_30m_pct < 0 ? "negative" : "positive"}">${signed(r.return_30m_pct, "%")}</strong></div>
      <div><span>Z</span><strong>${Number(r.z_score_30m || 0).toFixed(2)}</strong></div>
      <div><span>Range</span><strong>${Number(r.range_position_pct || 0).toFixed(1)}%</strong></div>
    </div>`;
}

function componentTitle(c = {}) {
  return [
    `Pivot ${c.pivot_extension || 0}`,
    `VWAP ${c.vwap_extreme || 0}`,
    `30m ${c.statistical_move || 0}`,
    `ATR ${c.daily_atr_move || 0}`,
    `Range ${c.range_extension || 0}`,
    `Liquidity ${c.volume_confirmation || 0}`,
    `Reversal ${c.reversal_evidence || 0}`,
    `Penalty -${c.trend_penalty || 0}`,
  ].join(" | ");
}

function renderMiniChart(row) {
  const chart = row.mini_chart || {};
  const points = Array.isArray(chart.points) ? chart.points : [];
  const width = 430;
  const height = 86;
  const pad = { left: 12, right: 10, top: 10, bottom: 18 };
  const prevW = 78;
  const gapW = 24;
  const sessionX0 = pad.left + prevW + gapW;
  const start = new Date(chart.session_start || 0).getTime();
  const end = new Date(chart.session_end || 0).getTime();
  const priorClose = Number(chart.prior_close || 0);
  const values = [];
  if (priorClose > 0) values.push(priorClose);
  points.forEach((p) => {
    if (Number(p.price) > 0) values.push(Number(p.price));
    if (Number(p.vwap) > 0) values.push(Number(p.vwap));
  });
  if (!values.length || !Number.isFinite(start) || !Number.isFinite(end) || end <= start) {
    return `<div class="mini-chart empty-chart">No regular-session chart data</div>`;
  }

  let min = Math.min(...values);
  let max = Math.max(...values);
  const spread = Math.max(max - min, max * 0.002, 0.01);
  min -= spread * 0.12;
  max += spread * 0.12;
  const innerW = width - sessionX0 - pad.right;
  const innerH = height - pad.top - pad.bottom;
  const x = (time) => sessionX0 + ((new Date(time).getTime() - start) / (end - start)) * innerW;
  const y = (value) => pad.top + (1 - ((value - min) / (max - min))) * innerH;
  const path = (key) => points
    .filter((p) => Number(p[key]) > 0)
    .map((p, i) => `${i === 0 ? "M" : "L"}${x(p.time).toFixed(1)} ${y(Number(p[key])).toFixed(1)}`)
    .join(" ");

  const pricePath = path("price");
  const vwapPath = path("vwap");
  const priorY = y(priorClose);
  const first = points.find((p) => Number(p.price) > 0);
  const gapColor = first && Number(first.price) >= priorClose ? "#14724f" : "#a33d3d";
  const gap = first && priorClose > 0
    ? `<line x1="${(sessionX0 - gapW).toFixed(1)}" y1="${priorY.toFixed(1)}" x2="${sessionX0.toFixed(1)}" y2="${y(Number(first.price)).toFixed(1)}" class="gap-line" stroke="${gapColor}"/>`
    : "";
  const prevStub = priorClose > 0
    ? `<path d="M${pad.left} ${priorY.toFixed(1)} L${(sessionX0 - gapW).toFixed(1)} ${priorY.toFixed(1)}" class="prev-day-line"></path>`
    : "";

  return `
    <div class="mini-chart">
      <svg width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img" aria-label="${row.symbol} regular-session mini chart">
        <rect x="0" y="0" width="${width}" height="${height}" rx="4" class="chart-bg"></rect>
        <rect x="${pad.left}" y="${pad.top}" width="${prevW}" height="${innerH}" class="prev-day-band"></rect>
        <line x1="${sessionX0}" y1="${pad.top}" x2="${sessionX0}" y2="${height - pad.bottom}" class="open-line"></line>
        <line x1="${pad.left}" y1="${height - pad.bottom}" x2="${width - pad.right}" y2="${height - pad.bottom}" class="axis-line"></line>
        ${priorClose > 0 ? `<line x1="${pad.left}" y1="${priorY.toFixed(1)}" x2="${width - pad.right}" y2="${priorY.toFixed(1)}" class="prev-close-line"></line>` : ""}
        ${prevStub}
        ${gap}
        ${vwapPath ? `<path d="${vwapPath}" class="vwap-line"></path>` : ""}
        ${pricePath ? `<path d="${pricePath}" class="price-line"></path>` : ""}
        <text x="${pad.left + 5}" y="${height - 5}" class="chart-time">Prev close</text>
        <text x="${sessionX0 - 9}" y="${height - 5}" class="chart-time">09:30</text>
        <text x="${width - 42}" y="${height - 5}" class="chart-time">16:00</text>
      </svg>
      <div class="mini-chart-legend">
        <span><i class="legend-price"></i>Price</span>
        <span><i class="legend-vwap"></i>VWAP</span>
        <span><i class="legend-prev"></i>Prior close</span>
      </div>
    </div>`;
}

function setMode(mode) {
  state.mode = mode;
  $("liveBtn").classList.toggle("active", mode === "live");
  $("histBtn").classList.toggle("active", mode === "historical");
  $("historicalPanel").classList.toggle("hidden", mode !== "historical");
  $("livePlayback").classList.toggle("hidden", mode !== "live");
  updateLiveControls();
  loadRankings().catch(showError);
}

function scheduleLiveRefresh() {
  clearTimeout(state.timer);
  state.timer = null;
  if (state.mode !== "live" || state.livePinned) return;
  state.timer = setTimeout(() => {
    loadRankings().catch(showError);
  }, state.refreshMs);
}

function dateTimeParts(date) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "America/New_York",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(date).reduce((acc, part) => {
    acc[part.type] = part.value;
    return acc;
  }, {});
  return {
    date: `${parts.year}-${parts.month}-${parts.day}`,
    time: `${parts.hour}:${parts.minute}`,
  };
}

function addMinutes(date, minutes) {
  return new Date(date.getTime() + minutes * 60_000);
}

function ensureLiveCursor() {
  if (!state.liveCursor) {
    state.liveCursor = state.lastLiveAsOf ? new Date(state.lastLiveAsOf) : new Date();
  }
}

function currentLiveCursor() {
  return state.lastLiveAsOf ? new Date(state.lastLiveAsOf) : new Date();
}

function setLivePinned(value) {
  state.livePinned = value;
  if (value) ensureLiveCursor();
  updateLiveControls();
  scheduleLiveRefresh();
}

function stepLive(minutes) {
  ensureLiveCursor();
  const next = addMinutes(state.liveCursor, minutes);
  if (minutes > 0 && state.lastLiveAsOf && next > state.lastLiveAsOf) {
    state.liveCursor = new Date(state.lastLiveAsOf);
  } else {
    state.liveCursor = next;
  }
  setLivePinned(true);
  loadRankings().catch(showError);
}

function pauseLive() {
  if (!state.livePinned) {
    state.liveCursor = currentLiveCursor();
    state.livePinned = true;
    updateLiveControls();
    $("dataClockMode").textContent = dataClockModeLabel("live paused");
    $("status").textContent = $("status").textContent.replace(/^live - /, "live paused - ");
  }
}

function goLiveLatest() {
  state.livePinned = false;
  updateLiveControls();
  loadRankings().catch(showError);
}

function updateLiveControls() {
  const isLiveMode = state.mode === "live";
  $("livePlayback").classList.toggle("hidden", !isLiveMode);
  $("pauseLiveBtn").classList.toggle("active", isLiveMode && state.livePinned);
  $("pauseLiveBtn").textContent = state.livePinned ? "Paused" : "Pause";
}

function sliderToTime(value) {
  const minutes = Number(value);
  const hh = Math.floor(minutes / 60);
  const mm = minutes % 60;
  return `${String(hh).padStart(2, "0")}:${String(mm).padStart(2, "0")}`;
}

function timeToSlider(value) {
  const [hh, mm] = value.split(":").map(Number);
  return hh * 60 + mm;
}

function startReplay() {
  if (state.playing) return;
  state.playing = true;
  state.playTimer = setInterval(() => {
    const slider = $("timeSlider");
    const next = Math.min(Number(slider.value) + 1, Number(slider.max));
    slider.value = String(next);
    $("histTime").value = sliderToTime(next);
    loadRankings().catch(showError);
    if (next >= Number(slider.max)) stopReplay();
  }, 1000);
}

function stopReplay() {
  state.playing = false;
  clearInterval(state.playTimer);
  state.playTimer = null;
}

function showError(err) {
  $("status").textContent = err.message || String(err);
}

async function refreshNow() {
  if (state.mode === "live") {
    if (state.livePinned) {
      await loadRankings();
      return;
    }
    const res = await fetch("/api/refresh", { method: "POST", cache: "no-store" });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || "refresh failed");
    renderSnapshot(data);
    scheduleLiveRefresh();
    return;
  }
  await loadRankings();
}

$("liveBtn").addEventListener("click", () => setMode("live"));
$("histBtn").addEventListener("click", () => setMode("historical"));
$("refreshBtn").addEventListener("click", () => refreshNow().catch(showError));
$("back1Btn").addEventListener("click", () => stepLive(-1));
$("pauseLiveBtn").addEventListener("click", pauseLive);
$("forward1Btn").addEventListener("click", () => stepLive(1));
$("latestBtn").addEventListener("click", goLiveLatest);
$("invertOrderToggle").addEventListener("change", (event) => {
  state.fadesFirst = event.target.checked;
  renderRows(state.lastRows || []);
});
$("histDate").addEventListener("change", () => loadRankings().catch(showError));
$("histTime").addEventListener("change", () => {
  $("timeSlider").value = String(timeToSlider($("histTime").value));
  loadRankings().catch(showError);
});
$("timeSlider").addEventListener("input", () => {
  $("histTime").value = sliderToTime($("timeSlider").value);
});
$("timeSlider").addEventListener("change", () => loadRankings().catch(showError));
$("playBtn").addEventListener("click", startReplay);
$("stopBtn").addEventListener("click", stopReplay);

loadConfig()
  .then(loadRankings)
  .catch(showError);
