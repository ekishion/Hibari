/**
 * 即刻轻盘 - 零依赖原生 SVG 二维码生成引擎
 * 支持字节编码模式 (Byte mode)，内置 Reed-Solomon 纠错与多版本自适应。
 * 输出纯矢量 SVG，无任何外部 CDN 或 npm 依赖。
 */
(function (root, factory) {
  if (typeof define === 'function' && define.amd) {
    define([], factory);
  } else if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.QRCode = factory();
  }
})(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Galois Field GF(256) 表
  const EXP_TABLE = new Uint8Array(512);
  const LOG_TABLE = new Uint8Array(256);
  (function initGF() {
    let x = 1;
    for (let i = 0; i < 255; i++) {
      EXP_TABLE[i] = x;
      LOG_TABLE[x] = i;
      x <<= 1;
      if (x & 0x100) x ^= 0x11d;
    }
    for (let i = 255; i < 512; i++) {
      EXP_TABLE[i] = EXP_TABLE[i - 255];
    }
  })();

  function gmult(a, b) {
    if (a === 0 || b === 0) return 0;
    return EXP_TABLE[LOG_TABLE[a] + LOG_TABLE[b]];
  }

  // 计算 Reed-Solomon 生成多项式
  function rsGeneratorPoly(n) {
    let poly = [1];
    for (let i = 0; i < n; i++) {
      const next = new Array(poly.length + 1).fill(0);
      const root = EXP_TABLE[i];
      for (let j = 0; j < poly.length; j++) {
        next[j] ^= gmult(poly[j], root);
        next[j + 1] ^= poly[j];
      }
      poly = next;
    }
    return poly;
  }

  // Reed-Solomon 编码
  function rsEncode(data, ecCount) {
    const gen = rsGeneratorPoly(ecCount);
    const msg = new Array(data.length + ecCount).fill(0);
    for (let i = 0; i < data.length; i++) msg[i] = data[i];

    for (let i = 0; i < data.length; i++) {
      const coef = msg[i];
      if (coef !== 0) {
        for (let j = 0; j < gen.length; j++) {
          msg[i + j] ^= gmult(gen[j], coef);
        }
      }
    }
    return msg.slice(data.length);
  }

  // 常见版本配置 (Level M): [版本, 模块数, 数据容量字节, EC码字数, 块数]
  // 足够容纳常见 200+ 字符的 URL
  const VERSION_CAPACITIES_M = [
    null,
    { ver: 1, size: 21, dataBytes: 16, ecBytes: 10, blocks: 1 },
    { ver: 2, size: 25, dataBytes: 28, ecBytes: 16, blocks: 1 },
    { ver: 3, size: 29, dataBytes: 44, ecBytes: 26, blocks: 1 },
    { ver: 4, size: 33, dataBytes: 64, ecBytes: 18, blocks: 2 },
    { ver: 5, size: 37, dataBytes: 86, ecBytes: 24, blocks: 2 },
    { ver: 6, size: 41, dataBytes: 108, ecBytes: 16, blocks: 4 },
    { ver: 7, size: 45, dataBytes: 124, ecBytes: 18, blocks: 4 },
    { ver: 8, size: 49, dataBytes: 154, ecBytes: 22, blocks: 4 },
    { ver: 9, size: 53, dataBytes: 182, ecBytes: 22, blocks: 5 },
    { ver: 10, size: 57, dataBytes: 216, ecBytes: 26, blocks: 5 },
  ];

  // 位置对齐图案坐标
  const ALIGN_COORDS = [
    [],
    [],
    [6, 18],
    [6, 22],
    [6, 26],
    [6, 30],
    [6, 34],
    [6, 22, 38],
    [6, 24, 42],
    [6, 26, 46],
    [6, 28, 50],
  ];

  class BitBuffer {
    constructor() {
      this.buffer = [];
      this.length = 0;
    }
    put(num, length) {
      for (let i = 0; i < length; i++) {
        this.putBit(((num >>> (length - i - 1)) & 1) === 1);
      }
    }
    putBit(bit) {
      const bufIndex = Math.floor(this.length / 8);
      if (this.buffer.length <= bufIndex) {
        this.buffer.push(0);
      }
      if (bit) {
        this.buffer[bufIndex] |= (0x80 >>> (this.length % 8));
      }
      this.length++;
    }
    getBytes() {
      return this.buffer;
    }
  }

  function pickVersion(dataLen) {
    for (let v = 1; v < VERSION_CAPACITIES_M.length; v++) {
      const cap = VERSION_CAPACITIES_M[v];
      // 模式指示(4) + 字符计数(8) + 数据长度
      if (dataLen + 2 <= cap.dataBytes) {
        return cap;
      }
    }
    return VERSION_CAPACITIES_M[VERSION_CAPACITIES_M.length - 1];
  }

  function encodeData(utf8Bytes, cfg) {
    const bb = new BitBuffer();
    // 8-bit Byte 模式指示符 = 0100
    bb.put(4, 4);
    // 字符计数 (版本 1-9 为 8 位)
    bb.put(utf8Bytes.length, cfg.ver <= 9 ? 8 : 16);
    for (let i = 0; i < utf8Bytes.length; i++) {
      bb.put(utf8Bytes[i], 8);
    }
    // 终止符
    const remain = cfg.dataBytes * 8 - bb.length;
    bb.put(0, Math.min(4, remain));
    // 补齐字节对齐
    while (bb.length % 8 !== 0) {
      bb.putBit(false);
    }
    // 填充交替码字
    const padPatterns = [0xec, 0x11];
    let padIdx = 0;
    while (bb.length < cfg.dataBytes * 8) {
      bb.put(padPatterns[padIdx % 2], 8);
      padIdx++;
    }
    return bb.getBytes();
  }

  function createMatrix(cfg, dataBytes) {
    const size = cfg.size;
    const m = Array.from({ length: size }, () => new Array(size).fill(null));

    function setModule(r, c, val) {
      if (r >= 0 && r < size && c >= 0 && c < size) m[r][c] = val;
    }

    // 1. 寻像图案 (Finder Patterns)
    function addFinder(top, left) {
      for (let r = -1; r <= 7; r++) {
        for (let c = -1; c <= 7; c++) {
          const row = top + r;
          const col = left + c;
          if (row < 0 || row >= size || col < 0 || col >= size) continue;
          if (
            (r >= 0 && r <= 6 && (c === 0 || c === 6)) ||
            (c >= 0 && c <= 6 && (r === 0 || r === 6)) ||
            (r >= 2 && r <= 4 && c >= 2 && c <= 4)
          ) {
            setModule(row, col, true);
          } else {
            setModule(row, col, false);
          }
        }
      }
    }
    addFinder(0, 0);
    addFinder(0, size - 7);
    addFinder(size - 7, 0);

    // 2. 定时图案 (Timing Patterns)
    for (let i = 8; i < size - 8; i++) {
      if (m[6][i] === null) m[6][i] = i % 2 === 0;
      if (m[i][6] === null) m[i][6] = i % 2 === 0;
    }

    // 3. 对齐图案 (Alignment Patterns)
    const aligns = ALIGN_COORDS[cfg.ver] || [];
    for (let i = 0; i < aligns.length; i++) {
      for (let j = 0; j < aligns.length; j++) {
        const r = aligns[i];
        const c = aligns[j];
        if (m[r][c] !== null) continue;
        for (let dr = -2; dr <= 2; dr++) {
          for (let dc = -2; dc <= 2; dc++) {
            const isBorder = Math.abs(dr) === 2 || Math.abs(dc) === 2;
            const isCenter = dr === 0 && dc === 0;
            setModule(r + dr, c + dc, isBorder || isCenter);
          }
        }
      }
    }

    // 4. 暗模块
    setModule(4 * cfg.ver + 9, 8, true);

    // 5. 格式信息预留 (掩码 000, 纠错 M = 00)
    for (let i = 0; i < 9; i++) {
      if (m[8][i] === null) m[8][i] = false;
      if (m[i][8] === null) m[i][8] = false;
    }
    for (let i = 0; i < 8; i++) {
      if (m[8][size - 1 - i] === null) m[8][size - 1 - i] = false;
      if (m[size - 1 - i][8] === null) m[size - 1 - i][8] = false;
    }

    // 6. 分块并交错放置数据与 EC
    const blocksCount = cfg.blocks;
    const dataPerBlock = Math.floor(cfg.dataBytes / blocksCount);
    const ecPerBlock = cfg.ecBytes;

    const rawBlocks = [];
    const ecBlocks = [];
    for (let b = 0; b < blocksCount; b++) {
      const blkData = dataBytes.slice(b * dataPerBlock, (b + 1) * dataPerBlock);
      rawBlocks.push(blkData);
      ecBlocks.push(rsEncode(blkData, ecPerBlock));
    }

    const interleaved = [];
    for (let i = 0; i < dataPerBlock; i++) {
      for (let b = 0; b < blocksCount; b++) interleaved.push(rawBlocks[b][i]);
    }
    for (let i = 0; i < ecPerBlock; i++) {
      for (let b = 0; b < blocksCount; b++) interleaved.push(ecBlocks[b][i]);
    }

    // 7. 填入数据模块（自右向左，蛇形穿行，采用标准掩码 (row + col) % 2 === 0）
    let bitIdx = 0;
    const totalBits = interleaved.length * 8;
    for (let right = size - 1; right > 0; right -= 2) {
      if (right === 6) right--; // 避开垂直定时线
      for (let vert = 0; vert < size; vert++) {
        for (let colOffset = 0; colOffset < 2; colOffset++) {
          const c = right - colOffset;
          const upward = ((right + 1) & 2) === 0;
          const r = upward ? size - 1 - vert : vert;
          if (m[r][c] === null) {
            let bit = false;
            if (bitIdx < totalBits) {
              const byteVal = interleaved[Math.floor(bitIdx / 8)];
              bit = ((byteVal >>> (7 - (bitIdx % 8))) & 1) === 1;
              bitIdx++;
            }
            // 应用掩码 0: (row + col) % 2 === 0
            if ((r + c) % 2 === 0) bit = !bit;
            m[r][c] = bit;
          }
        }
      }
    }

    // 8. 写入格式信息 (纠错等级 M=00, 掩码 0=000 => 格式字 0x5412)
    const formatBits = [1, 0, 1, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 1, 0];
    for (let i = 0; i < 15; i++) {
      const bit = formatBits[i] === 1;
      if (i <= 5) setModule(8, i, bit);
      else if (i === 6) setModule(8, 7, bit);
      else if (i === 7) setModule(8, 8, bit);
      else if (i === 8) setModule(7, 8, bit);
      else setModule(14 - i, 8, bit);

      if (i < 8) setModule(size - 1 - i, 8, bit);
      else setModule(8, size - 15 + i, bit);
    }

    return m;
  }

  function utf8Encode(str) {
    if (typeof TextEncoder !== 'undefined') {
      return new TextEncoder().encode(str);
    }
    const res = [];
    for (let i = 0; i < str.length; i++) {
      let code = str.charCodeAt(i);
      if (code < 0x80) res.push(code);
      else if (code < 0x800) {
        res.push(0xc0 | (code >> 6), 0x80 | (code & 0x3f));
      } else if (code < 0xd800 || code >= 0xe000) {
        res.push(0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f));
      } else {
        i++;
        code = 0x10000 + (((code & 0x3ff) << 10) | (str.charCodeAt(i) & 0x3ff));
        res.push(
          0xf0 | (code >> 18),
          0x80 | ((code >> 12) & 0x3f),
          0x80 | ((code >> 6) & 0x3f),
          0x80 | (code & 0x3f)
        );
      }
    }
    return new Uint8Array(res);
  }

  return {
    generateSVG: function (text, options) {
      options = options || {};
      const margin = typeof options.margin === 'number' ? options.margin : 2;
      const size = options.size || 200;
      const fgColor = options.fgColor || 'currentColor';
      const bgColor = options.bgColor || 'transparent';

      const utf8 = utf8Encode(text);
      const cfg = pickVersion(utf8.length);
      const encoded = encodeData(utf8, cfg);
      const matrix = createMatrix(cfg, encoded);
      const n = matrix.length;
      const totalSize = n + margin * 2;

      let pathData = '';
      for (let r = 0; r < n; r++) {
        for (let c = 0; c < n; c++) {
          if (matrix[r][c]) {
            pathData += `M${c + margin},${r + margin}h1v1h-1z `;
          }
        }
      }

      return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${totalSize} ${totalSize}" width="${size}" height="${size}" shape-rendering="crispEdges" aria-label="二维码">` +
        (bgColor !== 'transparent' ? `<rect width="${totalSize}" height="${totalSize}" fill="${bgColor}"/>` : '') +
        `<path d="${pathData.trim()}" fill="${fgColor}"/>` +
        `</svg>`;
    },

    renderTo: function (element, text, options) {
      if (!element) return;
      element.innerHTML = this.generateSVG(text, options);
    }
  };
});
