import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, describe, expect, it, vi } from "vitest"
import { PlanQueuePage } from "../PlanQueuePage"

const refresh = vi.fn()
afterEach(cleanup)

vi.mock("../../hooks/usePlan",()=>({usePlanQueue:()=>({data:[{plan_id:"pl-1",task_id:"task-1",name:"",title:"Search plan",summary:"Add FTS",status:"proposed",revision:2,unanswered_required:1,updated_at:""}],loading:false,error:null,refresh})}))

describe("PlanQueuePage",()=>{it("links proposed plans and shows unresolved choices",()=>{render(<MemoryRouter><PlanQueuePage/></MemoryRouter>);expect(screen.getByRole("heading",{name:"Search plan"})).toBeTruthy();expect(screen.getByText("1 required")).toBeTruthy();expect(screen.getByRole("link").getAttribute("href")).toBe("/plans/pl-1")})})

it("refreshes plans through a named control", () => { render(<MemoryRouter><PlanQueuePage/></MemoryRouter>); fireEvent.click(screen.getByRole("button", {name: "Refresh plan queue"})); expect(refresh).toHaveBeenCalledOnce() })
