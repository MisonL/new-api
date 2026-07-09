const REASONING_LEVELS = [
  { effort: 'low', description: 'Fast' },
  { effort: 'medium', description: 'Balanced' },
  { effort: 'high', description: 'Deep' },
];

export function buildModelCatalog(modelIds) {
  return {
    models: modelIds.map((modelId, index) => buildCatalogModel(modelId, index)),
  };
}

export function buildCatalogModel(modelId, index = 0) {
  if (typeof modelId !== 'string' || !modelId.trim()) {
    throw new Error('modelId 必须是非空字符串');
  }
  const normalizedModelId = modelId.trim();
  return {
    slug: normalizedModelId,
    display_name: normalizedModelId,
    description: 'Provided by new-api.',
    default_reasoning_level: 'medium',
    supported_reasoning_levels: REASONING_LEVELS.map((level) => ({ ...level })),
    shell_type: 'shell_command',
    visibility: 'list',
    supported_in_api: true,
    priority: 50 + index,
    additional_speed_tiers: [],
    service_tiers: [],
    availability_nux: null,
    upgrade: null,
    base_instructions: 'You are Codex, a coding agent.',
    model_messages: {
      instructions_template: 'You are Codex, a coding agent.\n\n{{ personality }}',
    },
    supports_reasoning_summaries: true,
    default_reasoning_summary: 'none',
    support_verbosity: true,
    default_verbosity: 'low',
    apply_patch_tool_type: 'freeform',
    web_search_tool_type: 'text_and_image',
    truncation_policy: { mode: 'tokens', limit: 10000 },
    supports_parallel_tool_calls: true,
    supports_image_detail_original: true,
    context_window: 200000,
    max_context_window: 200000,
    effective_context_window_percent: 95,
    experimental_supported_tools: [],
    input_modalities: ['text', 'image'],
    supports_search_tool: true,
    use_responses_lite: false,
  };
}
