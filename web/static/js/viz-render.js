// ABOUTME: Shared DOT rendering utilities for viz.js graph rendering.
// ABOUTME: Used by build_view and final_view for pipeline diagram visualization.

(function() {
    'use strict';

    /**
     * Render a DOT string to an SVG string using the viz.js library.
     * Handles viz-js v3 standalone (Viz.instance()) and legacy (new Viz()) APIs.
     * Returns a Promise that resolves to an SVG string.
     */
    function renderDOTToSVG(dotText) {
        // viz-js v3 standalone: Viz.instance().then(viz => viz.renderString(...))
        if (typeof Viz !== 'undefined' && Viz && typeof Viz.instance === 'function') {
            return Viz.instance().then(function(viz) {
                return renderWithVizInstance(viz, dotText);
            });
        }
        // legacy API: new Viz().renderString(...)
        if (typeof Viz === 'function') {
            try {
                var legacyViz = new Viz();
                var out = renderWithVizInstance(legacyViz, dotText);
                if (out && typeof out.then === 'function') {
                    return out;
                }
                return Promise.resolve(out);
            } catch (err) {
                return Promise.reject(err);
            }
        }
        return Promise.reject(new Error('viz runtime unavailable'));
    }

    function renderWithVizInstance(viz, dotText) {
        if (!viz) {
            return Promise.reject(new Error('viz instance unavailable'));
        }

        // Try renderString first.
        if (typeof viz.renderString === 'function') {
            try {
                var out = viz.renderString(dotText);
                if (out && typeof out.then === 'function') {
                    return out.then(normalizeRenderedSVG);
                }
                return Promise.resolve(normalizeRenderedSVG(out));
            } catch (_err) {
                // fall through to renderSVGElement
            }
        }

        // Fallback: render SVG element then serialize.
        if (typeof viz.renderSVGElement === 'function') {
            try {
                var svgOut = viz.renderSVGElement(dotText);
                if (svgOut && typeof svgOut.then === 'function') {
                    return svgOut.then(function(el) {
                        return normalizeRenderedSVG(el);
                    });
                }
                return Promise.resolve(normalizeRenderedSVG(svgOut));
            } catch (err2) {
                return Promise.reject(err2);
            }
        }

        return Promise.reject(new Error('viz instance missing render methods'));
    }

    function normalizeRenderedSVG(out) {
        if (typeof out === 'string') {
            var s = out.trim();
            if (s.indexOf('<svg') !== -1) {
                return s;
            }
            throw new Error('renderer returned non-SVG string');
        }

        if (out && typeof out === 'object') {
            if (typeof out.outerHTML === 'string' && out.outerHTML.indexOf('<svg') !== -1) {
                return out.outerHTML;
            }
            if (typeof out.svg === 'string' && out.svg.indexOf('<svg') !== -1) {
                return out.svg;
            }
            if (typeof out.output === 'string' && out.output.indexOf('<svg') !== -1) {
                return out.output;
            }
        }

        throw new Error('renderer returned unsupported output type');
    }

    /**
     * Normalize a DOT source string that may have been stored as a JSON-quoted
     * string with escaped newlines and quotes.
     */
    function normalizeDOTSource(raw) {
        var text = String(raw || '').trim();
        if (!text) {
            return text;
        }

        // Handle DOT accidentally stored as a quoted JSON string.
        if ((text.startsWith('"') && text.endsWith('"')) || (text.startsWith("'") && text.endsWith("'"))) {
            try {
                var parsed = JSON.parse(text);
                if (typeof parsed === 'string' && parsed.trim()) {
                    text = parsed.trim();
                }
            } catch (_err) {
                text = text.slice(1, -1).trim();
            }
        }

        // If escaped newlines survived storage, convert them back.
        if (text.indexOf('\\n') >= 0 && text.indexOf('\n') < 0) {
            text = text.replaceAll('\\n', '\n');
        }
        if (text.indexOf('\\"') >= 0) {
            text = text.replaceAll('\\"', '"');
        }
        return text;
    }

    /**
     * Escape a string for safe insertion into HTML.
     */
    function escapeHTML(s) {
        return String(s)
            .replaceAll('&', '&amp;')
            .replaceAll('<', '&lt;')
            .replaceAll('>', '&gt;')
            .replaceAll('"', '&quot;')
            .replaceAll("'", '&#39;');
    }

    /**
     * Highlight the active node in a rendered SVG graph.
     * Adds 'is-active' class to the matching node and removes it from all others.
     *
     * @param {string} nodeID - The node ID to highlight.
     * @param {Element} containerEl - The DOM element containing the rendered SVG.
     * @param {Element} [statusEl] - Optional element to update with active node text.
     */
    function setActiveNodeHighlight(nodeID, containerEl, statusEl) {
        if (!nodeID || !containerEl) {
            return;
        }
        var normalizedID = normalizeGraphNodeID(nodeID);
        var nodeEls = containerEl.querySelectorAll('g.node');
        if (!nodeEls.length) {
            return;
        }
        var highlighted = false;
        nodeEls.forEach(function(el) {
            el.classList.remove('is-active');
            var title = el.querySelector('title');
            var renderedID = title ? normalizeGraphNodeID(title.textContent) : '';
            if (renderedID && renderedID === normalizedID) {
                el.classList.add('is-active');
                highlighted = true;
            }
        });
        if (highlighted && statusEl) {
            statusEl.textContent = 'Active: ' + normalizedID;
        }
    }

    function normalizeGraphNodeID(raw) {
        var s = String(raw || '').trim();
        if (!s) {
            return '';
        }
        if ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'"))) {
            s = s.slice(1, -1).trim();
        }
        return s;
    }

    // Expose as window.mammothViz IIFE
    window.mammothViz = {
        renderDOTToSVG: renderDOTToSVG,
        normalizeDOTSource: normalizeDOTSource,
        escapeHTML: escapeHTML,
        setActiveNodeHighlight: setActiveNodeHighlight
    };
})();
