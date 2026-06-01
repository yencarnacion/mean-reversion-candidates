const state = {
  mode: "live",
  timer: null,
  playing: false,
  playTimer: null,
  livePinned: false,
  liveCursor: null,
  lastLiveAsOf: null,
};

const $ = (id) => document.getElementById(id);

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
  const res = await fetch("/api/config");
  const data = await res.json();
  renderFormula(data.formula);
  $("symbolCount").textContent = data.symbols.length;

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
  const pinnedLive = state.mode === "live" && state.livePinned;
  const url = state.mode === "historical" ? historicalUrl() : pinnedLive ? liveCursorUrl() : "/api/rankings";
  const res = await fetch(url);
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || "request failed");
  if (pinnedLive) {
    data.mode = "live paused";
  }
  renderSnapshot(data);
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
  renderRows(data.rankings || []);
}

function dataClockModeLabel(apiMode) {
  if (state.mode === "historical") return "Historical replay minute";
  if (state.livePinned) return "Live paused minute";
  if (apiMode === "live paused") return "Live paused minute";
  return "Latest live minute";
}

function renderRows(rows) {
  $("rankings").innerHTML = rows.map((r) => {
    const gradeClass = (r.grade || "").toLowerCase();
    const sideClass = r.side === "Long bounce" ? "side-long" : r.side === "Short fade" ? "side-short" : "";
    return `
      <tr class="stock-main-row">
        <td>${r.rank || "-"}</td>
        <td><a href="${r.chart_url || "#"}" target="_blank" rel="noreferrer">${r.symbol}</a></td>
        <td class="${sideClass}">${r.side}</td>
        <td><span class="score"><span class="grade ${gradeClass}">${r.grade}</span><strong>${Number(r.score || 0).toFixed(1)}</strong></span></td>
        <td>${renderPrice(r)}</td>
        <td>${Number(r.vwap || 0).toFixed(2)}</td>
        <td class="${r.move_from_vwap_pct < 0 ? "negative" : "positive"}">${signed(r.move_from_vwap_pct, "%")}</td>
        <td class="${r.day_move_atr < 0 ? "negative" : "positive"}">${signed(r.day_move_atr, "x")}</td>
        <td class="${r.vwap_stretch_atr < 0 ? "negative" : "positive"}">${signed(r.vwap_stretch_atr, "x")}</td>
        <td>${Number(r.atr_percent || 0).toFixed(2)}%</td>
        <td class="${r.return_30m_pct < 0 ? "negative" : "positive"}">${signed(r.return_30m_pct, "%")}</td>
        <td>${Number(r.z_score_30m || 0).toFixed(2)}</td>
        <td>${Number(r.range_position_pct || 0).toFixed(1)}%</td>
        <td>${fmtMoney(r.dollar_volume || 0)}</td>
      </tr>
      <tr class="stock-chart-row">
        <td colspan="14">${renderMiniChart(r)}</td>
      </tr>
      <tr class="stock-why-row">
        <td colspan="14">
          <div class="why-line">
            <span class="why-label">Why</span>
            <span>${r.reason || ""}</span>
            <span class="why-components">${componentTitle(r.components)}</span>
          </div>
        </td>
      </tr>`;
  }).join("");
}

function componentTitle(c = {}) {
  return [
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
  const now = new Date();
  if (!state.lastLiveAsOf) return now;
  const lastLiveAsOf = new Date(state.lastLiveAsOf);
  return lastLiveAsOf > now ? lastLiveAsOf : now;
}

function setLivePinned(value) {
  state.livePinned = value;
  if (value) ensureLiveCursor();
  updateLiveControls();
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
  }
  setLivePinned(true);
  loadRankings().catch(showError);
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
    const res = await fetch("/api/refresh", { method: "POST" });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || "refresh failed");
    renderSnapshot(data);
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

state.timer = setInterval(() => {
  if (state.mode === "live" && !state.livePinned) loadRankings().catch(showError);
}, 60_000);
