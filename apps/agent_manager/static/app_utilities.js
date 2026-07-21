// ============================================================
// Utilities
// ============================================================

function base64ToUtf8(b64) {
    const binary = atob(b64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return new TextDecoder('utf-8').decode(bytes);
}

function summarizeJobParams(params) {
    if (!params || typeof params !== 'object') return { short: '—', full: '' };
    const keys = Object.keys(params);
    if (!keys.length) return { short: '—', full: '' };
    // Build full JSON for tooltip
    const full = JSON.stringify(params, null, 2);
    // Build short summary: show first 2-3 key=value pairs, truncating long values
    const parts = [];
    for (const k of keys) {
        let v = params[k];
        if (v && typeof v === 'object') v = JSON.stringify(v);
        v = String(v);
        if (v.length > 30) v = v.slice(0, 27) + '…';
        parts.push(k + '=' + v);
        if (parts.length >= 3) break;
    }
    let short = parts.join(', ');
    if (keys.length > 3) short += ', …';
    return { short, full };
}

function escHtml(s) {
    const div = document.createElement('div');
    div.textContent = s;
    return div.innerHTML;
}

function escAttr(s) {
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/'/g, '&#39;')
        .replace(/"/g, '&quot;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;');
}

function invalidDateFallback(value) {
    return typeof value === 'string' ? value.trim() : '';
}

function parseDateValue(value) {
    if (value instanceof Date) {
        if (isNaN(value.getTime())) return null;
        return { date: new Date(value.getTime()), hasTime: true };
    }
    if (typeof value === 'number' && Number.isFinite(value)) {
        const millis = value < 1e12 ? value * 1000 : value;
        const date = new Date(millis);
        if (isNaN(date.getTime())) return null;
        return { date, hasTime: true };
    }

    const raw = String(value || '').trim();
    if (!raw) return null;

    if (/^\d+$/.test(raw)) {
        let millis = Number(raw);
        if (!Number.isFinite(millis)) return null;
        if (raw.length <= 10) millis *= 1000;
        const date = new Date(millis);
        if (isNaN(date.getTime())) return null;
        return { date, hasTime: true };
    }

    const dateOnlyMatch = raw.match(/^(\d{4})-(\d{2})-(\d{2})$/);
    if (dateOnlyMatch) {
        const year = Number(dateOnlyMatch[1]);
        const month = Number(dateOnlyMatch[2]);
        const day = Number(dateOnlyMatch[3]);
        const date = new Date(year, month - 1, day);
        if (date.getFullYear() !== year || date.getMonth() !== month - 1 || date.getDate() !== day) {
            return null;
        }
        return { date, hasTime: false };
    }

    const date = new Date(raw);
    if (isNaN(date.getTime())) return null;
    return { date, hasTime: true };
}

function startOfLocalDay(date) {
    return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

function localDayDifference(a, b) {
    return Math.round((startOfLocalDay(a).getTime() - startOfLocalDay(b).getTime()) / (1000 * 60 * 60 * 24));
}

function formatLocalTime(date, includeSeconds) {
    return date.toLocaleTimeString([], {
        hour: 'numeric',
        minute: '2-digit',
        ...(includeSeconds ? { second: '2-digit' } : {}),
    });
}

function formatCalendarDate(date, includeYear) {
    return date.toLocaleDateString([], {
        month: 'short',
        day: 'numeric',
        ...(includeYear ? { year: 'numeric' } : {}),
    });
}

function formatDatePrecise(value) {
    const parsed = parseDateValue(value);
    if (!parsed) return invalidDateFallback(value);
    if (!parsed.hasTime) {
        return formatCalendarDate(parsed.date, true);
    }
    return parsed.date.toLocaleString([], {
        weekday: 'short',
        month: 'short',
        day: 'numeric',
        year: 'numeric',
        hour: 'numeric',
        minute: '2-digit',
        second: '2-digit',
        timeZoneName: 'short',
    });
}

function formatHeaderClock(value) {
    const parsed = parseDateValue(value);
    if (!parsed) return invalidDateFallback(value);
    return parsed.date.toLocaleString([], {
        weekday: 'short',
        month: 'short',
        day: 'numeric',
        hour: 'numeric',
        minute: '2-digit',
        second: '2-digit',
        timeZoneName: 'short',
    });
}

function formatDate(value) {
    const parsed = parseDateValue(value);
    if (!parsed) return invalidDateFallback(value);

    const d = parsed.date;
    const now = new Date();

    if (!parsed.hasTime) {
        return formatCalendarDate(d, d.getFullYear() !== now.getFullYear());
    }

    const dayDiff = localDayDifference(now, d);
    const timeText = formatLocalTime(d, false);

    if (dayDiff === 0) {
        return `Today at ${timeText}`;
    }
    if (dayDiff === 1) {
        return `Yesterday at ${timeText}`;
    }
    if (dayDiff > 1 && dayDiff < 7) {
        return `${d.toLocaleDateString([], { weekday: 'short' })} at ${timeText}`;
    }
    if (d.getFullYear() === now.getFullYear()) {
        return `${formatCalendarDate(d, false)} at ${timeText}`;
    }
    return `${formatCalendarDate(d, true)} at ${timeText}`;
}

function formatRecentDate(value) {
    const parsed = parseDateValue(value);
    if (!parsed) return invalidDateFallback(value);
    if (!parsed.hasTime) return formatDate(value);

    const now = new Date();
    const diffMs = now.getTime() - parsed.date.getTime();
    if (diffMs < 0) return formatDate(value);

    const diffMin = Math.floor(diffMs / 60000);
    if (diffMin < 1) return 'just now';
    if (diffMin < 60) return `${diffMin}m ago`;

    const dayDiff = localDayDifference(now, parsed.date);
    if (dayDiff === 0) {
        return `${Math.floor(diffMin / 60)}h ago`;
    }
    return formatDate(value);
}

function formatInterval(minutes) {
    if (minutes >= 1440) {
        const days = Math.floor(minutes / 1440);
        const rem = minutes % 1440;
        if (rem === 0) return `${days}d`;
        return `${days}d${formatInterval(rem)}`;
    }
    if (minutes >= 60) {
        const hours = Math.floor(minutes / 60);
        const rem = minutes % 60;
        if (rem === 0) return `${hours}h`;
        return `${hours}h${rem}m`;
    }
    return `${minutes}m`;
}

function formatRelativeTime(date) {
    const now = new Date();
    const diffMs = date - now;
    const diffMins = Math.round(diffMs / 60000);
    const futureDayDiff = localDayDifference(date, now);

    if (diffMins < -60 * 24) {
        const days = Math.round(-diffMins / (60 * 24));
        return `${days}d overdue`;
    }
    if (diffMins < -60) {
        const hours = Math.round(-diffMins / 60);
        return `${hours}h overdue`;
    }
    if (diffMins < 0) {
        return `${-diffMins}m overdue`;
    }
    if (diffMins < 60) {
        return `in ${diffMins}m`;
    }
    if (diffMins < 60 * 24) {
        const hours = Math.round(diffMins / 60);
        return `in ${hours}h`;
    }
    if (futureDayDiff === 1) {
        return `Tomorrow at ${formatLocalTime(date, false)}`;
    }
    return formatDate(date);
}

function formatDisplayNumber(value, maximumFractionDigits = 2) {
    const num = Number(value);
    if (!Number.isFinite(num)) return '0';
    return num.toLocaleString([], {
        minimumFractionDigits: 0,
        maximumFractionDigits,
    });
}

function formatUSD(value) {
    const num = Number(value);
    if (!Number.isFinite(num)) return '$0.00';
    return num.toLocaleString([], {
        style: 'currency',
        currency: 'USD',
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
    });
}

function formatShareAmount(value) {
    const num = Number(value);
    if (!Number.isFinite(num)) return '0';
    return num.toLocaleString([], {
        minimumFractionDigits: 0,
        maximumFractionDigits: 4,
    });
}

function formatPolymarketTimestamp(value) {
    return formatDate(value);
}
