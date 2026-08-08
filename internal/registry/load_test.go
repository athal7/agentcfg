// -- Reserved agent name: "plan" collides with omp's native plan-mode machinery --

func TestValidate_AgentNamePlanCollidesWithOmpReservedName(t *testing.T) {
	t.Parallel()

	// An agent named "plan" collides with omp's native plan-mode machinery
	// and will hang indefinitely when dispatched. This must be a hard error.
	reg := registry.NewRegistry()
	reg.AddClass("test", new(testAgent))

	_, err := reg.Load(strings.NewReader(`
agents:
  - name: plan
    class: test
`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `agent name "plan" collides with omp's native plan-mode machinery`)
}
