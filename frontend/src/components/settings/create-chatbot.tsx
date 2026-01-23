import { useState, useEffect } from "react"
import { http } from "@/services"
import { zodResolver } from "@hookform/resolvers/zod"
import { PlusCircle, ChevronDown, ChevronUp } from "lucide-react"
import { useForm } from "react-hook-form"
import { useTranslation } from "react-i18next"
import { z } from "zod"

import { setFormErrors } from "@/lib/utils"
import { useEmitter } from "@/hooks/useEmitter"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import { Form, FormField, FormMessage } from "@/components/form/form"

import { Button } from "../ui/button"
import { Input } from "../ui/input"
import { Label } from "../ui/label"
import { Textarea } from "../ui/textarea"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select"
import { Checkbox } from "../ui/checkbox"
import { Switch } from "../ui/switch"
import { Slider } from "../ui/slider"
import { useToast } from "../ui/use-toast"
import { IProvider } from "@/types/provider"
import { ITool } from "@/types/tool"
import { IDocument } from "@/types/document"

  
export default function CreateChatbot() {
  const { toast } = useToast()
  const emitter = useEmitter()
  const { t } = useTranslation("settings")

  const [providers, setProviders] = useState<IProvider[]>([])
  const [models, setModels] = useState<string[]>([])
  const [tools, setTools] = useState<ITool[]>([])
  const [documents, setDocuments] = useState<IDocument[]>([])
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [newSuggestion, setNewSuggestion] = useState("")

  const formSchema = z.object({
    name: z.string().min(2, { message: t("chatbots.validation.name_min") }).max(255, { message: t("chatbots.validation.name_max") }),
    description: z.string().max(1000, { message: t("chatbots.validation.description_max") }).optional(),
    provider_id: z.number().min(1, t("chatbots.validation.provider_required")),
    model_name: z.string().min(1, t("chatbots.validation.model_required")),
    system_prompt: z.string().optional(),
    web_search_enabled: z.boolean(),
    tool_format: z.string(),
    output_format: z.string(),
    tool_ids: z.array(z.number()),
    document_ids: z.array(z.number()),
    prompt_suggestions: z.array(z.string()),
    // Model Parameters
    temperature: z.number().min(0).max(2).nullable(),
    top_p: z.number().min(0).max(1).nullable(),
    top_k: z.number().min(1).max(100).nullable(),
  })

  type FormValues = z.infer<typeof formSchema>

  const form = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      name: "",
      description: "",
      provider_id: 0,
      model_name: "",
      system_prompt: "",
      web_search_enabled: false,
      tool_format: "json",
      output_format: "json",
      tool_ids: [],
      document_ids: [],
      prompt_suggestions: [],
      temperature: null,
      top_p: null,
      top_k: null,
    },
  })

  const watchProviderId = form.watch("provider_id")
  const watchToolIds = form.watch("tool_ids")
  const watchDocumentIds = form.watch("document_ids")
  const watchPromptSuggestions = form.watch("prompt_suggestions")

  useEffect(() => {
    // Fetch providers
    http.get('/providers').then((res) => {
      setProviders(res.data.records || [])
    }).catch((e) => {
      console.error('Error fetching providers:', e)
    })

    // Fetch tools
    http.get('/tools?per_page=9999').then((res) => {
      setTools(res.data.records || [])
    }).catch((e) => {
      console.error('Error fetching tools:', e)
    })

    // Fetch documents
    http.get('/documents?per_page=9999&status=ready').then((res) => {
      setDocuments(res.data.records || [])
    }).catch((e) => {
      console.error('Error fetching documents:', e)
    })
  }, [])

  // Fetch models when provider changes
  useEffect(() => {
    if (watchProviderId) {
      http.get(`/providers/${watchProviderId}/models`).then((res) => {
        setModels(res.data.models || [])
      }).catch((e) => {
        console.error('Error fetching models:', e)
        setModels([])
      })
    } else {
      setModels([])
    }
  }, [watchProviderId])

  const [open, setOpen] = useState<boolean>(false)
  
  const handleCreate = (values: FormValues) => {
    http
      .post(`/chatbots`, values)
      .then((res) => {
        if (res.status === 200) {
          toast({
            title: t("success"),
            description: t("chatbots.toasts.success_msg"),
          })
          emitter.emit("REFETCH_CHATBOTS")
          setOpen(false)
          form.reset()
          setNewSuggestion("")
          setShowAdvanced(false)
        } else {
          toast({
            title: t("error"),
            description: t("chatbots.toasts.error_msg"),
            variant: "destructive",
          })
        }
      })
      .catch((e) => {
        if (!setFormErrors(e, form)) {
          toast({
            title: t("error"),
            description: t("chatbots.toasts.error_msg"),
            variant: "destructive",
          })
        }
      })
  }

  return (
    <Sheet open={open} onOpenChange={(o) => setOpen(o)}>
      <SheetTrigger asChild>
        <Button variant="outline" size="sm" className="ml-auto h-8 lg:flex">
          <PlusCircle className="mr-2 size-4" />
          {t("chatbots.create.button")}
        </Button>
      </SheetTrigger>
      <SheetContent side="right">
        <SheetHeader className="mb-8">
          <SheetTitle>{t("chatbots.create.title")}</SheetTitle>
          <SheetDescription>{t("chatbots.create.description")}</SheetDescription>
        </SheetHeader>

        <Form {...form}>
          <form
            onSubmit={form.handleSubmit(handleCreate)}
            className="space-y-5"
          >
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="name">{t("chatbots.create.name")}</Label>
                  <Input id="name" placeholder={t("chatbots.create.name_placeholder")} {...field} />
                  <FormMessage className="mt-1" />
                </div>
              )}
            />

            <FormField
              control={form.control}
              name="description"
              render={({ field }) => (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="description">{t("chatbots.create.description_field")}</Label>
                  <Textarea
                    id="description"
                    placeholder={t("chatbots.create.description_placeholder")}
                    {...field}
                  />
                  <FormMessage className="mt-1" />
                </div>
              )}
            />

            <FormField
              control={form.control}
              name="provider_id"
              render={({ field }) => (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="provider_id">{t("chatbots.create.provider")}</Label>
                  <Select
                    onValueChange={(value) => field.onChange(parseInt(value))}
                    value={field.value?.toString()}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder={t("chatbots.create.provider_placeholder")} />
                    </SelectTrigger>
                    <SelectContent>
                      {providers.map((provider) => (
                        <SelectItem key={provider.id} value={provider.id.toString()}>
                          {provider.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormMessage className="mt-1" />
                </div>
              )}
            />

            <FormField
              control={form.control}
              name="model_name"
              render={({ field }) => (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="model_name">{t("chatbots.create.model_name")}</Label>
                  <Select
                    onValueChange={field.onChange}
                    value={field.value}
                    disabled={!watchProviderId || models.length === 0}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder={watchProviderId ? t("chatbots.create.model_select_placeholder") : t("chatbots.create.model_select_provider_first")} />
                    </SelectTrigger>
                    <SelectContent>
                      {models.map((model) => (
                        <SelectItem key={model} value={model}>
                          {model}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormMessage className="mt-1" />
                </div>
              )}
            />

            <FormField
              control={form.control}
              name="system_prompt"
              render={({ field }) => (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="system_prompt">{t("chatbots.create.system_prompt")}</Label>
                  <Textarea
                    id="system_prompt"
                    placeholder={t("chatbots.create.system_prompt_placeholder")}
                    {...field}
                  />
                  <FormMessage className="mt-1" />
                </div>
              )}
            />

            <FormField
              control={form.control}
              name="web_search_enabled"
              render={({ field }) => (
                <div className="flex flex-col gap-2">
                  <div className="flex items-center justify-between">
                    <div className="space-y-0.5">
                      <Label htmlFor="web_search">
                        {t("chatbots.create.web_search", "Web Search")}
                      </Label>
                      <p className="text-sm text-muted-foreground">
                        {t("chatbots.create.web_search_description", "Enable web search capability for this chatbot")}
                      </p>
                    </div>
                    <Switch
                      id="web_search"
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </div>
                </div>
              )}
            />

            <FormField
              control={form.control}
              name="tool_format"
              render={({ field }) => (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="tool_format">
                    {t("chatbots.create.tool_format", "Tool Format")}
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    {t("chatbots.create.tool_format_description", "Choose the format for tool definitions. TOON format uses 30-60% fewer tokens than JSON.")}
                  </p>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="tool_format">
                      <SelectValue placeholder={t("chatbots.create.tool_format_placeholder", "Select format")} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="json">JSON (Standard)</SelectItem>
                      <SelectItem value="toon">TOON (Token-efficient)</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              )}
            />

            <FormField
              control={form.control}
              name="output_format"
              render={({ field }) => (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="output_format">
                    {t("chatbots.create.output_format", "Tool Output Format")}
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    {t("chatbots.create.output_format_description", "Choose the format for tool outputs sent to the LLM. TOON format uses 30-60% fewer tokens than JSON.")}
                  </p>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger id="output_format">
                      <SelectValue placeholder={t("chatbots.create.output_format_placeholder", "Select format")} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="json">JSON (Standard)</SelectItem>
                      <SelectItem value="toon">TOON (Token-efficient)</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              )}
            />

            {/* Advanced Model Parameters */}
            <div className="flex flex-col gap-2">
              <Button
                type="button"
                variant="ghost"
                className="flex items-center justify-between w-full p-0 h-auto hover:bg-transparent"
                onClick={() => setShowAdvanced(!showAdvanced)}
              >
                <Label className="cursor-pointer">
                  {t("chatbots.create.advanced_settings", "Advanced Model Parameters")}
                </Label>
                {showAdvanced ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
              </Button>
              <p className="text-sm text-muted-foreground">
                {t("chatbots.create.advanced_settings_description", "Fine-tune the model's behavior with temperature, top-p, top-k, and other parameters")}
              </p>
            </div>

            {showAdvanced && (
              <div className="space-y-4 border rounded-md p-4 bg-muted/30">
                {/* Temperature */}
                <FormField
                  control={form.control}
                  name="temperature"
                  render={({ field }) => (
                    <div className="flex flex-col gap-2">
                      <div className="flex items-center justify-between">
                        <Label htmlFor="temperature">
                          {t("chatbots.create.temperature", "Temperature")}
                        </Label>
                        <span className="text-sm text-muted-foreground">
                          {field.value !== null ? field.value.toFixed(2) : t("chatbots.create.default", "Default")}
                        </span>
                      </div>
                      <p className="text-xs text-muted-foreground">
                        {t("chatbots.create.temperature_description", "Controls randomness. Lower values make responses more focused and deterministic.")}
                      </p>
                      <div className="flex items-center gap-2">
                        <Slider
                          id="temperature"
                          min={0}
                          max={2}
                          step={0.01}
                          value={[field.value ?? 0.7]}
                          onValueChange={(v) => field.onChange(v[0])}
                          className="flex-1"
                        />
                        <Button type="button" variant="ghost" size="sm" onClick={() => field.onChange(null)} className="text-xs h-6 px-2">
                          {t("chatbots.create.reset", "Reset")}
                        </Button>
                      </div>
                    </div>
                  )}
                />

                {/* Top P */}
                <FormField
                  control={form.control}
                  name="top_p"
                  render={({ field }) => (
                    <div className="flex flex-col gap-2">
                      <div className="flex items-center justify-between">
                        <Label htmlFor="top_p">
                          {t("chatbots.create.top_p", "Top P (Nucleus Sampling)")}
                        </Label>
                        <span className="text-sm text-muted-foreground">
                          {field.value !== null ? field.value.toFixed(2) : t("chatbots.create.default", "Default")}
                        </span>
                      </div>
                      <p className="text-xs text-muted-foreground">
                        {t("chatbots.create.top_p_description", "Controls diversity via nucleus sampling. 0.9 means consider top 90% probability mass.")}
                      </p>
                      <div className="flex items-center gap-2">
                        <Slider
                          id="top_p"
                          min={0}
                          max={1}
                          step={0.01}
                          value={[field.value ?? 0.9]}
                          onValueChange={(v) => field.onChange(v[0])}
                          className="flex-1"
                        />
                        <Button type="button" variant="ghost" size="sm" onClick={() => field.onChange(null)} className="text-xs h-6 px-2">
                          {t("chatbots.create.reset", "Reset")}
                        </Button>
                      </div>
                    </div>
                  )}
                />

                {/* Top K */}
                <FormField
                  control={form.control}
                  name="top_k"
                  render={({ field }) => (
                    <div className="flex flex-col gap-2">
                      <div className="flex items-center justify-between">
                        <Label htmlFor="top_k">
                          {t("chatbots.create.top_k", "Top K")}
                        </Label>
                        <span className="text-sm text-muted-foreground">
                          {field.value !== null ? field.value : t("chatbots.create.default", "Default")}
                        </span>
                      </div>
                      <p className="text-xs text-muted-foreground">
                        {t("chatbots.create.top_k_description", "Limits vocabulary to top K tokens. Lower values increase focus.")}
                      </p>
                      <div className="flex items-center gap-2">
                        <Slider
                          id="top_k"
                          min={1}
                          max={100}
                          step={1}
                          value={[field.value ?? 40]}
                          onValueChange={(v) => field.onChange(v[0])}
                          className="flex-1"
                        />
                        <Button type="button" variant="ghost" size="sm" onClick={() => field.onChange(null)} className="text-xs h-6 px-2">
                          {t("chatbots.create.reset", "Reset")}
                        </Button>
                      </div>
                    </div>
                  )}
                />
              </div>
            )}

            <div className="flex flex-col gap-2">
              <Label>{t("chatbots.create.prompt_suggestions", "Prompt Suggestions")}</Label>
              <p className="text-sm text-muted-foreground">
                {t("chatbots.create.prompt_suggestions_description", "Add suggestions that will appear when users start a new conversation")}
              </p>
              <div className="flex gap-2">
                <Input
                  value={newSuggestion}
                  onChange={(e) => setNewSuggestion(e.target.value)}
                  placeholder={t("chatbots.create.add_suggestion_placeholder", "Enter a prompt suggestion...")}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault()
                      if (newSuggestion.trim() && watchPromptSuggestions.length < 6) {
                        form.setValue("prompt_suggestions", [...watchPromptSuggestions, newSuggestion.trim()])
                        setNewSuggestion("")
                      }
                    }
                  }}
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    if (newSuggestion.trim() && watchPromptSuggestions.length < 6) {
                      form.setValue("prompt_suggestions", [...watchPromptSuggestions, newSuggestion.trim()])
                      setNewSuggestion("")
                    }
                  }}
                  disabled={!newSuggestion.trim() || watchPromptSuggestions.length >= 6}
                >
                  {t("chatbots.create.add", "Add")}
                </Button>
              </div>
              {watchPromptSuggestions.length > 0 && (
                <div className="flex flex-wrap gap-2 mt-2">
                  {watchPromptSuggestions.map((suggestion, index) => (
                    <div
                      key={index}
                      className="inline-flex items-center gap-2 rounded-md border bg-muted px-3 py-1.5 text-sm"
                    >
                      <span>{suggestion}</span>
                      <button
                        type="button"
                        onClick={() => {
                          form.setValue("prompt_suggestions", watchPromptSuggestions.filter((_, i) => i !== index))
                        }}
                        className="text-muted-foreground hover:text-foreground"
                      >
                        ×
                      </button>
                    </div>
                  ))}
                </div>
              )}
              {watchPromptSuggestions.length >= 6 && (
                <p className="text-xs text-muted-foreground">
                  {t("chatbots.create.max_suggestions", "Maximum 6 suggestions allowed")}
                </p>
              )}
            </div>

            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between">
                <Label>{t("chatbots.create.tools")}</Label>
                {tools.length > 0 && (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      if (watchToolIds.length === tools.length) {
                        form.setValue("tool_ids", [])
                      } else {
                        form.setValue("tool_ids", tools.map(tool => tool.id))
                      }
                    }}
                  >
                    {watchToolIds.length === tools.length ? t("chatbots.create.tools_deselect_all") : t("chatbots.create.tools_select_all")}
                  </Button>
                )}
              </div>
              <div className="space-y-2 max-h-40 overflow-y-auto border rounded-md p-3">
                {tools.map((tool) => (
                  <div key={tool.id} className="flex items-center space-x-2">
                    <Checkbox
                      id={`tool-${tool.id}`}
                      checked={watchToolIds.includes(tool.id)}
                      onCheckedChange={(checked) => {
                        if (checked) {
                          form.setValue("tool_ids", [...watchToolIds, tool.id])
                        } else {
                          form.setValue("tool_ids", watchToolIds.filter(id => id !== tool.id))
                        }
                      }}
                    />
                    <label
                      htmlFor={`tool-${tool.id}`}
                      className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
                    >
                      {tool.display_name}
                    </label>
                    <span className="text-xs text-muted-foreground">
                      ({tool.name})
                    </span>
                  </div>
                ))}
                {tools.length === 0 && (
                  <p className="text-sm text-muted-foreground">{t("chatbots.create.tools_none_available")}</p>
                )}
              </div>
            </div>

            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between">
                <Label>{t("chatbots.create.documents")}</Label>
                {documents.length > 0 && (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      if (watchDocumentIds.length === documents.length) {
                        form.setValue("document_ids", [])
                      } else {
                        form.setValue("document_ids", documents.map(doc => doc.id))
                      }
                    }}
                  >
                    {watchDocumentIds.length === documents.length ? t("chatbots.create.documents_deselect_all") : t("chatbots.create.documents_select_all")}
                  </Button>
                )}
              </div>
              <div className="space-y-2 max-h-40 overflow-y-auto border rounded-md p-3">
                {documents.map((doc) => (
                  <div key={doc.id} className="flex items-center space-x-2">
                    <Checkbox
                      id={`document-${doc.id}`}
                      checked={watchDocumentIds.includes(doc.id)}
                      onCheckedChange={(checked) => {
                        if (checked) {
                          form.setValue("document_ids", [...watchDocumentIds, doc.id])
                        } else {
                          form.setValue("document_ids", watchDocumentIds.filter(id => id !== doc.id))
                        }
                      }}
                    />
                    <label
                      htmlFor={`document-${doc.id}`}
                      className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
                    >
                      {doc.title}
                    </label>
                    <span className="text-xs text-muted-foreground">
                      ({doc.file_name})
                    </span>
                  </div>
                ))}
                {documents.length === 0 && (
                  <p className="text-sm text-muted-foreground">{t("chatbots.create.documents_none_available")}</p>
                )}
              </div>
            </div>

            <SheetFooter>
              <Button type="submit">
                <PlusCircle className="mr-2 size-4" />{" "}
                {t("chatbots.create.create")}
              </Button>
            </SheetFooter>
          </form>
        </Form>
      </SheetContent>
    </Sheet>
  )
}
