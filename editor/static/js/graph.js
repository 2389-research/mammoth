// ABOUTME: Handles d3-graphviz rendering and SVG interaction for graph visualization
// ABOUTME: Manages node/edge selection, highlighting, and htmx-driven property panel updates
//
// KNOWN LIMITATION: DOT rendering is performed client-side via d3-graphviz, which
// interprets SVG attributes from DOT source. Malicious DOT content (e.g. crafted label
// attributes) could inject SVG/HTML into the rendered output. Mitigating this fully
// would require server-side DOT sanitization or a sandboxed rendering iframe, which
// is tracked as a future improvement.

(function() {
    'use strict';

    function editorBasePath() {
        if (window.MAMMOTH_EDITOR_BASE_PATH) {
            return window.MAMMOTH_EDITOR_BASE_PATH;
        }
        const editorEl = document.querySelector('.editor');
        if (editorEl && editorEl.dataset && editorEl.dataset.basePath) {
            return editorEl.dataset.basePath;
        }
        const path = window.location.pathname || '';
        const idx = path.indexOf('/sessions/');
        if (idx > 0) {
            return path.slice(0, idx);
        }
        return '';
    }

    function normalizeElementId(rawId) {
        let id = (rawId || '').trim();
        if (!id) {
            return '';
        }
        if ((id.startsWith('"') && id.endsWith('"')) || (id.startsWith("'") && id.endsWith("'"))) {
            id = id.slice(1, -1);
        }
        return id.trim();
    }

    function parseNodeAttrs(dotSource) {
        const nodeAttrs = {};
        if (!dotSource) {
            return nodeAttrs;
        }

        const nodeStmt = /^\s*("([^"\\]|\\.)*"|[A-Za-z_][A-Za-z0-9_.:-]*)\s*\[([^\]]*)\]\s*;?/gm;
        let m;
        while ((m = nodeStmt.exec(dotSource)) !== null) {
            const id = normalizeElementId(m[1]);
            const attrText = m[3] || '';
            if (!id) {
                continue;
            }
            const attrs = {};
            const attrRe = /([A-Za-z_][A-Za-z0-9_.-]*)\s*=\s*("[^"]*"|'[^']*'|[^,\]]+)/g;
            let a;
            while ((a = attrRe.exec(attrText)) !== null) {
                const key = (a[1] || '').trim().toLowerCase();
                const val = normalizeElementId(a[2]);
                if (key) {
                    attrs[key] = val;
                }
            }
            nodeAttrs[id] = attrs;
        }
        return nodeAttrs;
    }

    function inferNodeType(attrs) {
        const shape = String((attrs && attrs.shape) || '').toLowerCase();
        const nodeType = String((attrs && attrs.type) || '').toLowerCase();
        const hasPrompt = !!String((attrs && attrs.prompt) || '').trim();
        const hasTool = !!String((attrs && attrs.tool) || '').trim();
        const hasInterviewer = !!String((attrs && attrs.interviewer) || '').trim();

        if (shape === 'mdiamond') return 'start';
        if (shape === 'msquare') return 'exit';
        if (shape === 'diamond') return 'conditional';
        if (nodeType === 'wait.human' || shape === 'hexagon' || hasInterviewer) return 'human';
        if (nodeType === 'tool' || hasTool || shape === 'parallelogram') return 'tool';
        if (nodeType === 'codergen' || hasPrompt) return 'codegen';
        return '';
    }

    function applyNodeTypeClasses(dotSource) {
        const svg = document.querySelector('#graph-container svg');
        if (!svg) {
            return;
        }
        const nodeAttrs = parseNodeAttrs(dotSource);
        svg.querySelectorAll('.node').forEach(node => {
            node.classList.remove('node-type-start', 'node-type-exit', 'node-type-codegen', 'node-type-conditional', 'node-type-human', 'node-type-tool');
            const title = node.querySelector('title');
            if (!title) {
                return;
            }
            const nodeId = normalizeElementId(title.textContent);
            const attrs = nodeAttrs[nodeId] || {};
            const nodeType = inferNodeType(attrs);
            if (nodeType) {
                node.classList.add('node-type-' + nodeType);
            }
        });
    }

    let graphviz = null;
    let selectedElement = null;

    // Initialize graphviz once DOM is ready
    function initGraphviz() {
        const container = document.getElementById('graph-container');
        if (!container) {
            console.warn('Graph container not found, skipping initialization');
            return;
        }
        const width = Math.max(320, container.clientWidth || 0);
        const height = Math.max(220, container.clientHeight || 0);

        if (!graphviz) {
            graphviz = d3.select('#graph-container')
                .graphviz()
                .zoom(true)
                .fit(true)
                .scale(1.0)
                .width(width)
                .height(height)
                .on('initEnd', () => {
                    console.log('Graphviz initialized');
                    renderGraph();
                });
        }
    }

    // Render the graph from the DOT source
    window.renderGraph = function() {
        const dotSource = document.getElementById('dot-source');
        const container = document.getElementById('graph-container');

        if (!dotSource || !container) {
            console.warn('DOT source or container not found');
            return;
        }

        const dot = dotSource.value || dotSource.textContent;
        if (!dot || dot.trim() === '') {
            console.warn('Empty DOT source');
            container.innerHTML = '<p class="no-graph">No graph to display</p>';
            return;
        }

        if (!graphviz) {
            initGraphviz();
            return;
        }
        graphviz
            .width(Math.max(320, container.clientWidth || 0))
            .height(Math.max(220, container.clientHeight || 0));

        try {
            graphviz
                .renderDot(dot)
                .on('end', () => {
                    console.log('Graph rendered');
                    applyNodeTypeClasses(dot);
                    attachClickHandlers();
                });
        } catch (err) {
            console.error('Error rendering graph:', err);
            // Use textContent to prevent XSS via crafted error messages
            var errorP = document.createElement('p');
            errorP.className = 'error';
            errorP.textContent = 'Error rendering graph: ' + err.message;
            container.innerHTML = '';
            container.appendChild(errorP);
        }
    };

    // Attach click handlers to SVG nodes and edges
    function attachClickHandlers() {
        const svg = document.querySelector('#graph-container svg');
        if (!svg) {
            return;
        }

        // Click handler for nodes
        svg.querySelectorAll('.node').forEach(node => {
            node.style.cursor = 'pointer';
            node.addEventListener('click', (e) => {
                e.stopPropagation();
                const title = node.querySelector('title');
                if (title) {
                    const nodeId = normalizeElementId(title.textContent);
                    handleNodeClick(nodeId, node);
                }
            });
        });

        // Click handler for edges
        svg.querySelectorAll('.edge').forEach(edge => {
            edge.style.cursor = 'pointer';
            edge.addEventListener('click', (e) => {
                e.stopPropagation();
                const title = edge.querySelector('title');
                if (title) {
                    const edgeId = normalizeElementId(title.textContent);
                    handleEdgeClick(edgeId, edge);
                }
            });
        });

        // Click on background to deselect
        svg.addEventListener('click', (e) => {
            if (e.target === svg || e.target.closest('polygon.bg')) {
                clearSelection();
            }
        });
    }

    // Handle node click - fetch edit form via htmx
    function handleNodeClick(nodeId, element) {
        clearSelection();
        selectedElement = element;
        element.classList.add('selected');
        const sessionID = document.querySelector('.editor').dataset.sessionId;
        const textEl = element.querySelector('text');
        const nodeLabel = textEl ? String(textEl.textContent || '').trim() : '';
        const query = `id=${encodeURIComponent(nodeId)}${nodeLabel ? `&label=${encodeURIComponent(nodeLabel)}` : ''}`;
        htmx.ajax('GET', `${editorBasePath()}/sessions/${sessionID}/node-edit?${query}`, {target: '#selection-props', swap: 'innerHTML'});
    }

    // Handle edge click - fetch edit form via htmx
    function handleEdgeClick(edgeId, element) {
        clearSelection();
        selectedElement = element;
        element.classList.add('selected');
        const sessionID = document.querySelector('.editor').dataset.sessionId;
        htmx.ajax('GET', `${editorBasePath()}/sessions/${sessionID}/edge-edit?id=${encodeURIComponent(edgeId)}`, {target: '#selection-props', swap: 'innerHTML'});
    }

    // Clear current selection
    function clearSelection() {
        if (selectedElement) {
            selectedElement.classList.remove('selected');
            selectedElement = null;
        }

        const propsPanel = document.getElementById('selection-props');
        if (propsPanel) {
            propsPanel.innerHTML = '<p class="hint">Click a node or edge in the graph to view its properties</p>';
        }
    }

    // Highlight a node
    window.highlightNode = function(nodeId) {
        const svg = document.querySelector('#graph-container svg');
        if (!svg) {
            return;
        }

        // Remove existing highlights
        svg.querySelectorAll('.highlighted').forEach(el => {
            el.classList.remove('highlighted');
        });

        // Find and highlight the node
        svg.querySelectorAll('.node').forEach(node => {
            const title = node.querySelector('title');
            if (title && title.textContent === nodeId) {
                node.classList.add('highlighted');
            }
        });
    };

    // Highlight an edge
    window.highlightEdge = function(edgeId) {
        const svg = document.querySelector('#graph-container svg');
        if (!svg) {
            return;
        }

        // Remove existing highlights
        svg.querySelectorAll('.highlighted').forEach(el => {
            el.classList.remove('highlighted');
        });

        // Find and highlight the edge
        svg.querySelectorAll('.edge').forEach(edge => {
            const title = edge.querySelector('title');
            if (title && title.textContent === edgeId) {
                edge.classList.add('highlighted');
            }
        });
    };

    // Initialize on DOM ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', () => {
            setTimeout(initGraphviz, 100);
        });
    } else {
        setTimeout(initGraphviz, 100);
    }

    console.log('Graph.js initialized');
})();
