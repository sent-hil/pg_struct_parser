import { useEffect, useState } from 'react'
import { SchemaViewer } from './components/SchemaViewer'
import styled from '@emotion/styled'
import sqlFile from './data/filtered_tables.sql?raw'

const LoadingContainer = styled.div`
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100vh;
  font-family: system-ui, -apple-system, sans-serif;
  flex-direction: column;
  gap: 20px;
  padding: 20px;
  text-align: center;
`

const ErrorDetails = styled.pre`
  background: #ffebee;
  padding: 20px;
  border-radius: 8px;
  max-width: 800px;
  overflow: auto;
  font-size: 14px;
  text-align: left;
`

function App () {
  const [sqlContent, setSqlContent] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [debugInfo, setDebugInfo] = useState<string>('')

  useEffect(() => {
    try {
      setDebugInfo('Loading SQL content from imported file...\n')

      if (!sqlFile) {
        throw new Error('SQL file import failed')
      }

      setDebugInfo(prev =>
        `${prev}Content preview (first 500 chars):\n` +
        `${sqlFile.slice(0, 500)}\n` +
        `Content length: ${sqlFile.length} chars\n` +
        `Contains 'CREATE TABLE': ${sqlFile.includes('CREATE TABLE')}\n` +
        `First CREATE TABLE index: ${sqlFile.indexOf('CREATE TABLE')}`
      )

      // Add more detailed debugging
      const createTableCount = (sqlFile.match(/CREATE TABLE/g) || []).length
      const statements = sqlFile.split(';').map(s => s.trim()).filter(Boolean)

      setDebugInfo(prev =>
        `${prev}\n\nAnalysis:\n` +
        `Number of CREATE TABLE statements found: ${createTableCount}\n` +
        `Number of SQL statements: ${statements.length}\n` +
        `First few statements:\n` +
        statements.slice(0, 3).map(s => `- ${s.slice(0, 100)}...`).join('\n')
      )

      if (!sqlFile.includes('CREATE TABLE')) {
        throw new Error('No CREATE TABLE statements found in SQL file')
      }

      setSqlContent(sqlFile)
      setDebugInfo(prev => `${prev}\nSQL content loaded successfully`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error occurred')
      setDebugInfo(prev => `${prev}\nError: ${err instanceof Error ? err.message : err}`)
    }
  }, [])

  if (error) {
    return (
      <LoadingContainer>
        <div>Error loading schema:</div>
        <ErrorDetails>{error}</ErrorDetails>
        <div>Debug Information:</div>
        <ErrorDetails>{debugInfo}</ErrorDetails>
      </LoadingContainer>
    )
  }

  if (!sqlContent) {
    return (
      <LoadingContainer>
        <div>Loading schema...</div>
        <ErrorDetails>{debugInfo}</ErrorDetails>
      </LoadingContainer>
    )
  }

  return <SchemaViewer sqlContent={sqlContent} />
}

export default App
