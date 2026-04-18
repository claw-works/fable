// 清水镇像素地图 - 纯 Canvas 渲染
// 瓦片大小
const TILE = 16;
const MAP_COLS = 60;
const MAP_ROWS = 45;

// 瓦片类型
const T = {
  GRASS: 0, PATH: 1, WATER: 2, BRIDGE: 3, WALL: 4,
  ROOF: 5, FLOOR: 6, DOOR: 7, TREE: 8, FLOWER: 9,
  FENCE: 10, STONE: 11,
};

// 瓦片颜色
const TILE_COLORS = {
  [T.GRASS]:  '#4a7c3f',
  [T.PATH]:   '#c4a265',
  [T.WATER]:  '#3b6ea5',
  [T.BRIDGE]: '#8b6914',
  [T.WALL]:   '#a0937d',
  [T.ROOF]:   '#8b2500',
  [T.FLOOR]:  '#d2b48c',
  [T.DOOR]:   '#654321',
  [T.TREE]:   '#2d5a1e',
  [T.FLOWER]: '#d4a0c0',
  [T.FENCE]:  '#8b7355',
  [T.STONE]:  '#808080',
};

// 地点在地图上的像素坐标（瓦片坐标）和区域
const LOCATIONS = {
  '茶馆':   { x: 12, y: 10, w: 8, h: 6, label: '🍵 茶馆' },
  '集市':   { x: 30, y: 18, w: 10, h: 5, label: '🏪 集市' },
  '铁匠铺': { x: 48, y: 28, w: 7, h: 5, label: '🔨 铁匠铺' },
  '私塾':   { x: 10, y: 28, w: 7, h: 5, label: '📖 私塾' },
  '城隍庙': { x: 30, y: 35, w: 8, h: 6, label: '🏛 城隍庙' },
  '渡口':   { x: 52, y: 8,  w: 5, h: 4, label: '⛵ 渡口' },
};

// 生成地图数据
function generateMap() {
  const map = [];
  for (let y = 0; y < MAP_ROWS; y++) {
    map[y] = [];
    for (let x = 0; x < MAP_COLS; x++) {
      map[y][x] = T.GRASS;
    }
  }

  // 河流（右侧纵向）
  for (let y = 0; y < MAP_ROWS; y++) {
    for (let dx = 0; dx < 3; dx++) {
      const wx = 56 + dx + Math.round(Math.sin(y * 0.3) * 1);
      if (wx >= 0 && wx < MAP_COLS) map[y][wx] = T.WATER;
    }
  }

  // 道路网络 - 连接各地点
  const paths = [
    // 茶馆 → 集市（水平）
    { from: { x: 20, y: 13 }, to: { x: 30, y: 20 } },
    // 茶馆 → 私塾（垂直）
    { from: { x: 14, y: 16 }, to: { x: 14, y: 28 } },
    // 集市 → 铁匠铺
    { from: { x: 40, y: 20 }, to: { x: 48, y: 30 } },
    // 私塾 → 城隍庙
    { from: { x: 17, y: 30 }, to: { x: 30, y: 37 } },
    // 铁匠铺 → 城隍庙
    { from: { x: 48, y: 33 }, to: { x: 38, y: 37 } },
    // 集市 → 渡口
    { from: { x: 40, y: 18 }, to: { x: 52, y: 10 } },
    // 茶馆 → 城隍庙（对角）
    { from: { x: 16, y: 16 }, to: { x: 30, y: 35 } },
  ];

  paths.forEach(p => drawPath(map, p.from, p.to));

  // 建筑
  for (const [, loc] of Object.entries(LOCATIONS)) {
    drawBuilding(map, loc.x, loc.y, loc.w, loc.h);
  }

  // 桥（渡口附近跨河）
  for (let dx = 0; dx < 5; dx++) {
    const bx = 55 + dx;
    if (bx < MAP_COLS) { map[9][bx] = T.BRIDGE; map[10][bx] = T.BRIDGE; }
  }

  // 散布树木
  for (let i = 0; i < 80; i++) {
    const tx = Math.floor(Math.random() * MAP_COLS);
    const ty = Math.floor(Math.random() * MAP_ROWS);
    if (map[ty][tx] === T.GRASS) map[ty][tx] = T.TREE;
  }

  // 散布花朵
  for (let i = 0; i < 30; i++) {
    const tx = Math.floor(Math.random() * MAP_COLS);
    const ty = Math.floor(Math.random() * MAP_ROWS);
    if (map[ty][tx] === T.GRASS) map[ty][tx] = T.FLOWER;
  }

  return map;
}

function drawPath(map, from, to) {
  // L 形路径
  const midX = to.x, midY = from.y;
  for (let x = Math.min(from.x, midX); x <= Math.max(from.x, midX); x++) {
    if (x >= 0 && x < MAP_COLS && from.y >= 0 && from.y < MAP_ROWS) {
      if (map[from.y][x] === T.GRASS) map[from.y][x] = T.PATH;
      if (from.y + 1 < MAP_ROWS && map[from.y + 1][x] === T.GRASS) map[from.y + 1][x] = T.PATH;
    }
  }
  for (let y = Math.min(midY, to.y); y <= Math.max(midY, to.y); y++) {
    if (to.x >= 0 && to.x < MAP_COLS && y >= 0 && y < MAP_ROWS) {
      if (map[y][to.x] === T.GRASS) map[y][to.x] = T.PATH;
      if (to.x + 1 < MAP_COLS && map[y][to.x + 1] === T.GRASS) map[y][to.x + 1] = T.PATH;
    }
  }
}

function drawBuilding(map, bx, by, bw, bh) {
  for (let y = by; y < by + bh && y < MAP_ROWS; y++) {
    for (let x = bx; x < bx + bw && x < MAP_COLS; x++) {
      if (y === by) map[y][x] = T.ROOF;
      else if (y === by + bh - 1 && x === bx + Math.floor(bw / 2)) map[y][x] = T.DOOR;
      else if (y === by + 1) map[y][x] = T.ROOF;
      else if (x === bx || x === bx + bw - 1 || y === by + bh - 1) map[y][x] = T.WALL;
      else map[y][x] = T.FLOOR;
    }
  }
}

// 渲染地图到 Canvas
function renderMap(ctx, map) {
  for (let y = 0; y < MAP_ROWS; y++) {
    for (let x = 0; x < MAP_COLS; x++) {
      ctx.fillStyle = TILE_COLORS[map[y][x]] || '#000';
      ctx.fillRect(x * TILE, y * TILE, TILE, TILE);
    }
  }
}

// 渲染地点标签
function renderLocationLabels(ctx) {
  ctx.font = 'bold 11px sans-serif';
  for (const [, loc] of Object.entries(LOCATIONS)) {
    const px = loc.x * TILE + (loc.w * TILE) / 2;
    const py = loc.y * TILE - 6;
    const text = loc.label;
    const tw = ctx.measureText(text).width;
    ctx.fillStyle = 'rgba(0,0,0,0.75)';
    ctx.fillRect(px - tw / 2 - 4, py - 10, tw + 8, 16);
    ctx.fillStyle = '#FFD700';
    ctx.textAlign = 'center';
    ctx.fillText(text, px, py);
    ctx.textAlign = 'left';
  }
}

// 判断瓦片是否可行走
function isWalkable(map, tx, ty) {
  if (tx < 0 || tx >= MAP_COLS || ty < 0 || ty >= MAP_ROWS) return false;
  const t = map[ty][tx];
  return t === T.PATH || t === T.FLOOR || t === T.DOOR || t === T.GRASS || t === T.BRIDGE || t === T.FLOWER;
}

// 获取地点中心瓦片坐标
function getLocationCenter(name) {
  const loc = LOCATIONS[name];
  if (!loc) return null;
  // 门口位置（建筑底部中间）
  return { x: loc.x + Math.floor(loc.w / 2), y: loc.y + loc.h };
}

// 在建筑内部散开角色，避免重叠
function getLocationSpread(name, index) {
  const loc = LOCATIONS[name];
  if (!loc) return null;
  // 在建筑内部 FLOOR 区域分散，留 1 格墙壁边距
  const innerW = Math.max(loc.w - 2, 1);
  const innerH = Math.max(loc.h - 2, 1);
  const col = index % innerW;
  const row = Math.floor(index / innerW) % innerH;
  return { x: loc.x + 1 + col, y: loc.y + 1 + row };
}

// 获取角色所在地点名称
function getLocationAt(tx, ty) {
  for (const [name, loc] of Object.entries(LOCATIONS)) {
    if (tx >= loc.x - 2 && tx <= loc.x + loc.w + 2 &&
        ty >= loc.y - 2 && ty <= loc.y + loc.h + 2) {
      return name;
    }
  }
  return null;
}
