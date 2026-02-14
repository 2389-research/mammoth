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
        return window.MAMMOTH_EDITOR_BASE_PATH || '';
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

        if (!graphviz) {
            graphviz = d3.select('#graph-container')
                .graphviz()
                .zoom(true)
                .fit(true)
                .scale(1.0)
                .width(container.clientWidth)
                .height(container.clientHeight || 600)
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

        try {
            graphviz
                .renderDot(dot)
                .on('end', () => {
                    console.log('Graph rendered');
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
                    const nodeId = title.textContent;
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
                    const edgeId = title.textContent;
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
        htmx.ajax('GET', `${editorBasePath()}/sessions/${sessionID}/nodes/${encodeURIComponent(nodeId)}/edit`, {target: '#selection-props', swap: 'innerHTML'});
    }

    // Handle edge click - fetch edit form via htmx
    function handleEdgeClick(edgeId, element) {
        clearSelection();
        selectedElement = element;
        element.classList.add('selected');
        const sessionID = document.querySelector('.editor').dataset.sessionId;
        // Edge IDs contain -> which needs URL encoding
        htmx.ajax('GET', `${editorBasePath()}/sessions/${sessionID}/edges/${encodeURIComponent(edgeId)}/edit`, {target: '#selection-props', swap: 'innerHTML'});
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
